package api

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultWorksPageSize = int64(25)

type adminWorkListItem struct {
	ID           int64      `json:"id"`
	PlatformID   string     `json:"platform_id"`
	ParseCount   int64      `json:"parse_count"`
	VersionCount int64      `json:"version_count"`
	LastParsedAt *time.Time `json:"last_parsed_at,omitempty"`
	Title        string     `json:"title,omitempty"`
	ThumbnailURL string     `json:"thumbnail_url,omitempty"`
}

type adminWorkPage struct {
	Items      []adminWorkListItem `json:"items"`
	NextCursor *string             `json:"next_cursor"`
}

type popularWorkView struct {
	PlatformID string `json:"platform_id"`
	Title      string `json:"title,omitempty"`
	WorkURL    string `json:"work_url"`
	ParseCount int64  `json:"parse_count"`
}

type popularWorksResponse struct {
	Enabled   bool              `json:"enabled"`
	AllTime   []popularWorkView `json:"all_time"`
	Recent30D []popularWorkView `json:"recent_30d"`
	Recent7D  []popularWorkView `json:"recent_7d"`
}

func (a *App) handleAdminWorks(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	if !a.requireAdmin(writer, request) {
		return
	}

	limit := defaultWorksPageSize
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 || value > 100 {
			writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_LIMIT", "limit 必须在 1 到 100 之间")
			return
		}
		limit = value
	}
	cursor, err := decodeWorkListCursor(request.URL.Query().Get("cursor"))
	if err != nil {
		writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_CURSOR", "cursor 无效")
		return
	}
	page, err := a.store.listWorks(request.Context(), limit, cursor)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "WORKS_READ_FAILED", "无法读取作品列表")
		return
	}

	response := adminWorkPage{Items: make([]adminWorkListItem, 0, len(page.Items))}
	for _, item := range page.Items {
		view := adminWorkListItem{
			ID: item.ID, PlatformID: item.PlatformID, ParseCount: item.ParseCount,
			VersionCount: item.VersionCount, LastParsedAt: item.LastParsedAt, Title: item.Title,
		}
		if item.ThumbnailResourceID != nil {
			view.ThumbnailURL = "/api/admin/v1/resources/" + strconv.FormatInt(*item.ThumbnailResourceID, 10) + "/content"
		}
		response.Items = append(response.Items, view)
	}
	if page.NextCursor != nil {
		value := encodeWorkListCursor(*page.NextCursor)
		response.NextCursor = &value
	}
	writeJSON(writer, http.StatusOK, response)
}

func encodeWorkListCursor(cursor workListCursor) string {
	raw := fmt.Sprintf("%d:%d", cursor.LastParsedAt, cursor.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeWorkListCursor(raw string) (workListCursor, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 128 {
		return workListCursor{}, errors.New("work cursor is too long")
	}
	if raw == "" {
		return workListCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return workListCursor{}, err
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) != 2 {
		return workListCursor{}, errors.New("invalid work cursor")
	}
	lastParsedAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || lastParsedAt < 0 {
		return workListCursor{}, errors.New("invalid work cursor timestamp")
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id < 1 {
		return workListCursor{}, errors.New("invalid work cursor ID")
	}
	return workListCursor{LastParsedAt: lastParsedAt, ID: id}, nil
}

func (a *App) handlePopularWorks(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	settings, err := a.store.loadSettings(request.Context())
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SETTINGS_READ_FAILED", "无法读取榜单设置")
		return
	}
	empty := popularWorksResponse{
		AllTime: make([]popularWorkView, 0), Recent30D: make([]popularWorkView, 0), Recent7D: make([]popularWorkView, 0),
	}
	if !settings.ShowPopular {
		writeJSON(writer, http.StatusOK, empty)
		return
	}
	_, authenticated, err := a.authenticatedSession(request)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SESSION_CHECK_FAILED", "无法校验管理员会话")
		return
	}
	if !settings.Public && !authenticated {
		writeAPIError(writer, http.StatusUnauthorized, "PUBLIC_ACCESS_DISABLED", "当前服务未开放匿名访问")
		return
	}

	allTime, err := a.store.popularWorks(request.Context(), 0, 10)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "POPULAR_WORKS_READ_FAILED", "无法读取累计热门榜单")
		return
	}
	recent30, err := a.store.popularWorks(request.Context(), 30, 10)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "POPULAR_WORKS_READ_FAILED", "无法读取近 30 天热门榜单")
		return
	}
	recent7, err := a.store.popularWorks(request.Context(), 7, 10)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "POPULAR_WORKS_READ_FAILED", "无法读取近 7 天热门榜单")
		return
	}
	writeJSON(writer, http.StatusOK, popularWorksResponse{
		Enabled: true, AllTime: popularWorkViews(allTime),
		Recent30D: popularWorkViews(recent30), Recent7D: popularWorkViews(recent7),
	})
}

func popularWorkViews(items []popularWork) []popularWorkView {
	result := make([]popularWorkView, 0, len(items))
	for _, item := range items {
		result = append(result, popularWorkView{
			PlatformID: item.PlatformID, Title: item.Title,
			WorkURL: canonicalWorkURL(item.PlatformID), ParseCount: item.ParseCount,
		})
	}
	return result
}

func (a *App) handleAdminResourceContent(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		methodNotAllowed(writer, "GET, HEAD")
		return
	}
	if !a.requireAdmin(writer, request) {
		return
	}
	rawPath := strings.TrimPrefix(request.URL.Path, "/api/admin/v1/resources/")
	parts := strings.Split(rawPath, "/")
	if len(parts) != 2 || parts[1] != "content" {
		http.NotFound(writer, request)
		return
	}
	resourceID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || resourceID < 1 {
		writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_RESOURCE_ID", "资源 ID 无效")
		return
	}
	resource, err := a.store.storedImageResource(request.Context(), resourceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeAPIError(writer, http.StatusNotFound, "RESOURCE_NOT_FOUND", "缩略图不存在")
		return
	}
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "RESOURCE_READ_FAILED", "无法读取缩略图")
		return
	}
	path, err := secureStoredResourcePath(a.config.VolumeDir, resource.RelativePath)
	if err != nil {
		a.logger.Printf("resolve stored resource %d: %v", resourceID, err)
		writeAPIError(writer, http.StatusNotFound, "RESOURCE_NOT_FOUND", "缩略图不存在")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeAPIError(writer, http.StatusNotFound, "RESOURCE_NOT_FOUND", "缩略图不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeAPIError(writer, http.StatusNotFound, "RESOURCE_NOT_FOUND", "缩略图不存在")
		return
	}
	if resource.SHA256 != "" {
		etag := `"` + resource.SHA256 + `"`
		writer.Header().Set("ETag", etag)
		if request.Header.Get("If-None-Match") == etag {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
	}
	writer.Header().Set("Cache-Control", "private, max-age=3600")
	writer.Header().Set("Content-Disposition", "inline")
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(resource.MIMEType, "image/") {
		writer.Header().Set("Content-Type", resource.MIMEType)
	}
	http.ServeContent(writer, request, filepath.Base(path), info.ModTime(), file)
}

func secureStoredResourcePath(volumeDir, relativePath string) (string, error) {
	if strings.TrimSpace(relativePath) == "" || filepath.IsAbs(relativePath) {
		return "", errors.New("stored resource path is invalid")
	}
	volume, err := filepath.Abs(volumeDir)
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(filepath.Join(volume, filepath.FromSlash(relativePath)))
	if err != nil || !pathContainedBy(volume, candidate) || candidate == volume {
		return "", errors.New("stored resource escapes volume")
	}
	realVolume, err := filepath.EvalSymlinks(volume)
	if err != nil {
		return "", err
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil || !pathContainedBy(realVolume, realCandidate) {
		return "", errors.New("stored resource symlink escapes volume")
	}
	info, err := os.Stat(realCandidate)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("stored resource is not a regular file")
	}
	return realCandidate, nil
}
