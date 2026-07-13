package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type App struct {
	config           Config
	logger           *log.Logger
	records          *recordStore
	store            *appStore
	handler          http.Handler
	startedAt        time.Time
	clientFactory    clientFactory
	clients          *httpClientPool
	downloads        *downloadCoordinator
	adminFingerprint [32]byte
	adminConfigured  bool
	loginLimiter     *adminLoginLimiter
	volumeLock       *os.File
	closeOnce        sync.Once
	closeErr         error
}

func New(config Config, logger *log.Logger) (*App, error) {
	config = config.withDefaults()
	if config.AdminPasswordRequired && config.AdminPassword == "" {
		return nil, errors.New("XHS_ADMIN_PASSWORD or XHS_ADMIN_PASSWORD_FILE must be configured")
	}
	if len(config.AdminUsername) > 64 || strings.ContainsAny(config.AdminUsername, "\r\n") {
		return nil, errors.New("XHS_ADMIN_USERNAME must be at most 64 characters without line breaks")
	}
	if logger == nil {
		logger = log.Default()
	}
	for _, directory := range []string{
		config.VolumeDir,
		filepath.Join(config.VolumeDir, "Download"),
		filepath.Join(config.VolumeDir, "Temp"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", directory, err)
		}
		if err := verifyWritableDirectory(directory); err != nil {
			return nil, err
		}
	}
	volumeLock, err := acquireVolumeLock(config.VolumeDir)
	if err != nil {
		return nil, err
	}
	records, err := openRecordStore(config.VolumeDir)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open download records: %w", err), releaseVolumeLock(volumeLock))
	}
	store, err := openAppStore(config)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open application database: %w", err), releaseVolumeLock(volumeLock))
	}
	adminFingerprint, adminConfigured := store.secrets.adminCredentialFingerprint(config.AdminUsername, config.AdminPassword)
	config.AdminPassword = ""
	clients := newHTTPClientPool(config.RequestTimeout, config.AllowPrivateProxy)
	app := &App{
		config:           config,
		logger:           logger,
		records:          records,
		store:            store,
		startedAt:        time.Now(),
		clients:          clients,
		volumeLock:       volumeLock,
		adminFingerprint: adminFingerprint,
		adminConfigured:  adminConfigured,
		loginLimiter:     newAdminLoginLimiter(),
		downloads: newDownloadCoordinator(defaultDownloadConcurrency, downloadLimits{
			totalTimeout:  config.DownloadTimeout,
			idleTimeout:   config.DownloadIdleTimeout,
			maxMediaBytes: config.MaxMediaBytes,
		}),
	}
	app.clientFactory = clients.Client
	app.handler = app.routes()
	return app, nil
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		var clientErr, storeErr error
		if a.clients != nil {
			clientErr = a.clients.Close()
		}
		if a.store != nil {
			storeErr = a.store.Close()
		}
		a.closeErr = errors.Join(clientErr, storeErr, releaseVolumeLock(a.volumeLock))
		a.volumeLock = nil
	})
	return a.closeErr
}

func (a *App) HTTPServer() *http.Server {
	return &http.Server{
		Addr:              a.config.Address(),
		Handler:           a.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       a.config.RequestTimeout + 5*time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}
}

func (a *App) Handler() http.Handler { return a.handler }

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/xhs/detail", a.handleDetail)
	mux.HandleFunc("/api/v1/access", a.handleAccess)
	mux.HandleFunc("/api/v1/extractions", a.handleExtractionsV1)
	mux.HandleFunc("/api/admin/v1/auth/session", a.handleAdminSession)
	mux.HandleFunc("/api/admin/v1/settings", a.handleAdminSettings)
	mux.HandleFunc("/api/admin/v1/history", a.handleAdminHistory)
	mux.HandleFunc("/api/admin/v1/works/", a.handleAdminWork)
	mux.HandleFunc("/openapi.json", a.handleOpenAPI)
	mux.HandleFunc("/docs", a.handleDocs)
	mux.HandleFunc("/redoc", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/docs", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/api/", func(writer http.ResponseWriter, request *http.Request) {
		writeAPIError(writer, http.StatusNotFound, "NOT_FOUND", "API 路由不存在")
	})
	mux.HandleFunc("/", a.handleWeb)
	return a.logRequests(mux)
}

func (a *App) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		next.ServeHTTP(writer, request)
		a.logger.Printf("%s %s %s", request.Method, request.URL.Path, time.Since(started).Round(time.Millisecond))
	})
}

func (a *App) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		a.logger.Printf("health database ping: %v", err)
		writeAPIError(writer, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "服务数据库不可用")
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"status":         "ok",
		"service":        "xhs-downloader-api",
		"uptime_seconds": int64(time.Since(a.startedAt).Seconds()),
	})
}

func (a *App) handleDetail(writer http.ResponseWriter, request *http.Request) {
	a.handleLegacyExtraction(writer, request)
}

func (a *App) writeExtract(writer http.ResponseWriter, message string, params ExtractParams, data map[string]any) {
	writeJSON(writer, http.StatusOK, ExtractResponse{Message: message, Params: params, Data: data})
}

func (a *App) handleOpenAPI(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writeJSON(writer, http.StatusOK, openAPIDocument())
}

func (a *App) handleDocs(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = writer.Write([]byte(docsHTML))
	}
}

func (a *App) handleWeb(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	index := filepath.Join(a.config.WebDistDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		if request.URL.Path != "/" {
			http.NotFound(writer, request)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{
			"service": "XHS-Downloader Go API",
			"docs":    "/docs",
			"health":  "/healthz",
		})
		return
	}

	relative := strings.TrimPrefix(filepath.Clean(request.URL.Path), string(filepath.Separator))
	candidate := filepath.Join(a.config.WebDistDir, relative)
	if relative != "." {
		if path, err := filepath.Rel(a.config.WebDistDir, candidate); err == nil && path != ".." && !strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				if strings.HasPrefix(request.URL.Path, "/assets/") {
					writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				http.ServeFile(writer, request, candidate)
				return
			}
		}
	}
	writer.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(writer, request, index)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func methodNotAllowed(writer http.ResponseWriter, allowed string) {
	writer.Header().Set("Allow", allowed)
	writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"message": "Method Not Allowed"})
}

func shutdownServer(ctx context.Context, server *http.Server) error {
	err := server.Shutdown(ctx)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
