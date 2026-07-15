package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	adminSessionCookie     = "xhs_admin_session"
	maxAdminBodyBytes      = int64(32 << 10)
	maxCookieBytes         = 16 << 10
	maxAdminUsernameBytes  = 64
	maxAdminPasswordBytes  = 1024
	maxLoginLimiterEntries = 4096
)

type adminLoginAttempt struct {
	Failures     int
	WindowStart  time.Time
	BlockedUntil time.Time
}

type adminLoginLimiter struct {
	mu       sync.Mutex
	attempts map[string]adminLoginAttempt
}

func newAdminLoginLimiter() *adminLoginLimiter {
	return &adminLoginLimiter{attempts: make(map[string]adminLoginAttempt)}
}

func (l *adminLoginLimiter) reserve(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	attempt, exists := l.attempts[key]
	if exists && !attempt.BlockedUntil.IsZero() && now.Before(attempt.BlockedUntil) {
		return false
	}
	if !exists && len(l.attempts) >= maxLoginLimiterEntries {
		l.evictOldestLocked()
	}
	if !exists || attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) >= 5*time.Minute {
		attempt = adminLoginAttempt{WindowStart: now}
	}
	if attempt.Failures >= 5 {
		attempt.BlockedUntil = now.Add(5 * time.Minute)
		l.attempts[key] = attempt
		return false
	}
	attempt.Failures++
	if attempt.Failures >= 5 {
		attempt.BlockedUntil = now.Add(5 * time.Minute)
	}
	l.attempts[key] = attempt
	return true
}

func (l *adminLoginLimiter) pruneLocked(now time.Time) {
	for key, attempt := range l.attempts {
		expiresAt := attempt.WindowStart.Add(5 * time.Minute)
		if attempt.BlockedUntil.After(expiresAt) {
			expiresAt = attempt.BlockedUntil
		}
		if !now.Before(expiresAt) {
			delete(l.attempts, key)
		}
	}
}

func (l *adminLoginLimiter) evictOldestLocked() {
	oldestKey := ""
	var oldest time.Time
	for key, attempt := range l.attempts {
		if oldestKey == "" || attempt.WindowStart.Before(oldest) {
			oldestKey = key
			oldest = attempt.WindowStart
		}
	}
	if oldestKey != "" {
		delete(l.attempts, oldestKey)
	}
}

func (l *adminLoginLimiter) succeeded(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

type sessionResponse struct {
	Authenticated bool       `json:"authenticated"`
	Username      string     `json:"username,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type configuredSecretView struct {
	Configured bool   `json:"configured"`
	Display    string `json:"display,omitempty"`
}

type saveSettingsView struct {
	Text   bool `json:"text"`
	Images bool `json:"images"`
	Videos bool `json:"videos"`
}

type settingsResponse struct {
	Revision      int64                `json:"revision"`
	Public        bool                 `json:"public"`
	ShowPopular   bool                 `json:"show_popular"`
	Save          saveSettingsView     `json:"save"`
	Refetch       bool                 `json:"refetch"`
	DefaultCookie configuredSecretView `json:"default_cookie"`
	DefaultProxy  configuredSecretView `json:"default_proxy"`
}

type secretPatchRequest struct {
	Action string  `json:"action"`
	Value  *string `json:"value,omitempty"`
}

type saveSettingsPatch struct {
	Text   *bool `json:"text,omitempty"`
	Images *bool `json:"images,omitempty"`
	Videos *bool `json:"videos,omitempty"`
}

type settingsPatchRequest struct {
	Revision      int64               `json:"revision"`
	Public        *bool               `json:"public,omitempty"`
	ShowPopular   *bool               `json:"show_popular,omitempty"`
	Save          *saveSettingsPatch  `json:"save,omitempty"`
	Refetch       *bool               `json:"refetch,omitempty"`
	DefaultCookie *secretPatchRequest `json:"default_cookie,omitempty"`
	DefaultProxy  *secretPatchRequest `json:"default_proxy,omitempty"`
}

func (a *App) handleAdminSession(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		a.handleSessionStatus(writer, request)
	case http.MethodPost:
		if !sameOriginRequest(request) {
			writeAPIError(writer, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "请求来源不受信任")
			return
		}
		a.handleSessionLogin(writer, request)
	case http.MethodDelete:
		if !sameOriginRequest(request) {
			writeAPIError(writer, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "请求来源不受信任")
			return
		}
		if _, authenticated, err := a.authenticatedSession(request); err != nil {
			writeAPIError(writer, http.StatusInternalServerError, "SESSION_CHECK_FAILED", "无法校验管理员会话")
			return
		} else if !authenticated {
			writeAPIError(writer, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "需要管理员登录")
			return
		}
		a.handleSessionLogout(writer, request)
	default:
		writer.Header().Set("Allow", "GET, POST, DELETE")
		writeAPIError(writer, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method Not Allowed")
	}
}

func (a *App) handleSessionStatus(writer http.ResponseWriter, request *http.Request) {
	expiresAt, authenticated, err := a.authenticatedSession(request)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SESSION_CHECK_FAILED", "无法校验管理员会话")
		return
	}
	response := sessionResponse{Authenticated: authenticated}
	if authenticated {
		response.Username = a.config.AdminUsername
		response.ExpiresAt = &expiresAt
	}
	writeJSON(writer, http.StatusOK, response)
}

func (a *App) handleSessionLogin(writer http.ResponseWriter, request *http.Request) {
	if !a.adminConfigured {
		writeAPIError(writer, http.StatusServiceUnavailable, "ADMIN_NOT_CONFIGURED", "管理员凭据未配置")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxAdminBodyBytes)
	var login loginRequest
	if err := decodeStrictJSON(request.Body, &login); err != nil {
		writeAPIError(writer, requestDecodeStatus(err), "INVALID_REQUEST", "登录请求无效")
		return
	}
	loginKey := loginRateLimitKey(request)
	now := time.Now().UTC()
	if !a.loginLimiter.reserve(loginKey, now) {
		writeAPIError(writer, http.StatusTooManyRequests, "LOGIN_RATE_LIMITED", "登录失败次数过多，请稍后重试")
		return
	}
	if len(login.Username) == 0 || len(login.Username) > maxAdminUsernameBytes ||
		len(login.Password) == 0 || len(login.Password) > maxAdminPasswordBytes {
		writeAPIError(writer, http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码错误")
		return
	}
	provided, _ := a.store.secrets.adminCredentialFingerprint(login.Username, login.Password)
	if subtle.ConstantTimeCompare(provided[:], a.adminFingerprint[:]) != 1 {
		writeAPIError(writer, http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码错误")
		return
	}
	a.loginLimiter.succeeded(loginKey)

	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SESSION_CREATE_FAILED", "无法创建管理员会话")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256.Sum256([]byte(token))
	expiresAt := now.Add(a.config.AdminSessionTTL)
	if err := a.store.purgeExpiredSessions(request.Context(), now); err != nil {
		a.logger.Printf("purge admin sessions: %v", err)
	}
	if err := a.store.createSession(
		request.Context(), tokenHash[:], a.adminFingerprint[:], now, expiresAt,
	); err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SESSION_CREATE_FAILED", "无法创建管理员会话")
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt,
		MaxAge:   int(a.config.AdminSessionTTL.Seconds()),
	})
	writeJSON(writer, http.StatusOK, sessionResponse{
		Authenticated: true,
		Username:      a.config.AdminUsername,
		ExpiresAt:     &expiresAt,
	})
}

func loginRateLimitKey(request *http.Request) string {
	host := request.RemoteAddr
	if parsed, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		host = parsed
	}
	return host
}

func (a *App) handleSessionLogout(writer http.ResponseWriter, request *http.Request) {
	if hash, ok := sessionHashFromRequest(request); ok {
		if err := a.store.deleteSession(request.Context(), hash[:]); err != nil {
			writeAPIError(writer, http.StatusInternalServerError, "SESSION_DELETE_FAILED", "无法注销管理员会话")
			return
		}
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
	writer.WriteHeader(http.StatusNoContent)
}

func (a *App) authenticatedSession(request *http.Request) (time.Time, bool, error) {
	if !a.adminConfigured {
		return time.Time{}, false, nil
	}
	hash, ok := sessionHashFromRequest(request)
	if !ok {
		return time.Time{}, false, nil
	}
	return a.store.sessionValid(request.Context(), hash[:], a.adminFingerprint[:], time.Now().UTC())
}

func sessionHashFromRequest(request *http.Request) ([sha256.Size]byte, bool) {
	cookie, err := request.Cookie(adminSessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256([]byte(cookie.Value)), true
}

func sameOriginRequest(request *http.Request) bool {
	if site := strings.TrimSpace(request.Header.Get("Sec-Fetch-Site")); site != "" &&
		site != "same-origin" && site != "none" {
		return false
	}
	rawOrigin := strings.TrimSpace(request.Header.Get("Origin"))
	if rawOrigin == "" {
		return true
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.User != nil || origin.Host == "" ||
		(origin.Scheme != "http" && origin.Scheme != "https") {
		return false
	}
	return strings.EqualFold(origin.Host, request.Host)
}

func (a *App) requireAdmin(writer http.ResponseWriter, request *http.Request) bool {
	_, authenticated, err := a.authenticatedSession(request)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SESSION_CHECK_FAILED", "无法校验管理员会话")
		return false
	}
	if !authenticated {
		writeAPIError(writer, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "需要管理员登录")
		return false
	}
	return true
}

func (a *App) handleAdminSettings(writer http.ResponseWriter, request *http.Request) {
	if !a.requireAdmin(writer, request) {
		return
	}
	switch request.Method {
	case http.MethodGet:
		settings, err := a.store.loadSettings(request.Context())
		if err != nil {
			writeAPIError(writer, http.StatusInternalServerError, "SETTINGS_READ_FAILED", "无法读取设置")
			return
		}
		writeJSON(writer, http.StatusOK, settingsAPIResponse(settings))
	case http.MethodPatch:
		if !sameOriginRequest(request) {
			writeAPIError(writer, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "请求来源不受信任")
			return
		}
		a.patchAdminSettings(writer, request)
	default:
		writer.Header().Set("Allow", "GET, PATCH")
		writeAPIError(writer, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method Not Allowed")
	}
}

func (a *App) patchAdminSettings(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxAdminBodyBytes)
	var patch settingsPatchRequest
	if err := decodeStrictJSON(request.Body, &patch); err != nil {
		writeAPIError(writer, requestDecodeStatus(err), "INVALID_REQUEST", "设置请求无效")
		return
	}
	if patch.Revision < 1 {
		writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_REVISION", "revision 必须为正整数")
		return
	}
	update := settingsUpdate{
		Revision:    patch.Revision,
		Public:      patch.Public,
		ShowPopular: patch.ShowPopular,
		Refetch:     patch.Refetch,
	}
	if patch.Save != nil {
		update.SaveText = patch.Save.Text
		update.SaveImages = patch.Save.Images
		update.SaveVideos = patch.Save.Videos
	}
	var err error
	if update.DefaultCookie, err = validateCookiePatch(patch.DefaultCookie); err != nil {
		writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_COOKIE", err.Error())
		return
	}
	if update.DefaultProxy, err = a.validateProxyPatch(patch.DefaultProxy); err != nil {
		writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_PROXY", err.Error())
		return
	}
	settings, err := a.store.updateSettings(request.Context(), update)
	if errors.Is(err, errSettingsRevisionConflict) {
		writeAPIError(writer, http.StatusConflict, "SETTINGS_REVISION_CONFLICT", "设置已被其他请求修改，请刷新后重试")
		return
	}
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SETTINGS_UPDATE_FAILED", "无法更新设置")
		return
	}
	writeJSON(writer, http.StatusOK, settingsAPIResponse(settings))
}

func validateCookiePatch(patch *secretPatchRequest) (*secretMutation, error) {
	if patch == nil {
		return nil, nil
	}
	action := strings.TrimSpace(patch.Action)
	switch action {
	case "keep", "clear":
		if patch.Value != nil {
			return nil, fmt.Errorf("%s 操作不能包含 value", action)
		}
		return &secretMutation{Action: action}, nil
	case "replace":
		if patch.Value == nil || strings.TrimSpace(*patch.Value) == "" {
			return nil, errors.New("replace 操作必须提供非空 value")
		}
		value := strings.TrimSpace(*patch.Value)
		if len(value) > maxCookieBytes {
			return nil, fmt.Errorf("Cookie 不能超过 %d 字节", maxCookieBytes)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("Cookie 不能包含换行符")
		}
		return &secretMutation{Action: action, Value: value}, nil
	default:
		return nil, errors.New("action 必须是 keep、replace 或 clear")
	}
}

func (a *App) validateProxyPatch(patch *secretPatchRequest) (*secretMutation, error) {
	if patch == nil {
		return nil, nil
	}
	action := strings.TrimSpace(patch.Action)
	switch action {
	case "keep", "clear":
		if patch.Value != nil {
			return nil, fmt.Errorf("%s 操作不能包含 value", action)
		}
		return &secretMutation{Action: action}, nil
	case "replace":
		if patch.Value == nil || strings.TrimSpace(*patch.Value) == "" {
			return nil, errors.New("replace 操作必须提供非空 value")
		}
		value := strings.TrimSpace(*patch.Value)
		if _, err := parseProxy(&value, a.config.AllowPrivateProxy); err != nil {
			return nil, err
		}
		return &secretMutation{Action: action, Value: value}, nil
	default:
		return nil, errors.New("action 必须是 keep、replace 或 clear")
	}
}

func settingsAPIResponse(settings runtimeSettings) settingsResponse {
	response := settingsResponse{
		Revision:    settings.Revision,
		Public:      settings.Public,
		ShowPopular: settings.ShowPopular,
		Save: saveSettingsView{
			Text: settings.SaveText, Images: settings.SaveImages, Videos: settings.SaveVideos,
		},
		Refetch:       settings.Refetch,
		DefaultCookie: configuredSecretView{Configured: settings.DefaultCookie != nil},
		DefaultProxy:  configuredSecretView{Configured: settings.DefaultProxy != nil},
	}
	if settings.DefaultProxy != nil {
		response.DefaultProxy.Display = redactedProxyDisplay(*settings.DefaultProxy)
	}
	return response
}

func redactedProxyDisplay(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (a *App) handleAdminHistory(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	if !a.requireAdmin(writer, request) {
		return
	}
	limit := int64(50)
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 || value > 100 {
			writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_LIMIT", "limit 必须在 1 到 100 之间")
			return
		}
		limit = value
	}
	cursor, err := parseCursor(request.URL.Query().Get("cursor"))
	if err != nil {
		writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_CURSOR", err.Error())
		return
	}
	page, err := a.store.history(request.Context(), limit, cursor)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "HISTORY_READ_FAILED", "无法读取解析历史")
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (a *App) handleAdminWork(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	if !a.requireAdmin(writer, request) {
		return
	}
	rawID := strings.TrimPrefix(request.URL.Path, "/api/admin/v1/works/")
	if rawID == "" || strings.Contains(rawID, "/") {
		http.NotFound(writer, request)
		return
	}
	workID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || workID < 1 {
		writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_WORK_ID", "作品 ID 无效")
		return
	}
	detail, err := a.store.workDetail(request.Context(), workID)
	if errors.Is(err, sql.ErrNoRows) {
		writeAPIError(writer, http.StatusNotFound, "WORK_NOT_FOUND", "作品不存在")
		return
	}
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "WORK_READ_FAILED", "无法读取作品历史")
		return
	}
	writeJSON(writer, http.StatusOK, detail)
}

func decodeStrictJSON(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func requestDecodeStatus(err error) int {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusUnprocessableEntity
}

func writeAPIError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]string{"code": code, "message": message})
}
