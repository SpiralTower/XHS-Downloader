package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxRequestedURLBytes = 8 << 10

type optionalConnectionString struct {
	Present  bool
	Disabled bool
	Value    string
}

func (o *optionalConnectionString) UnmarshalJSON(content []byte) error {
	o.Present = true
	content = bytes.TrimSpace(content)
	if bytes.Equal(content, []byte("null")) {
		o.Disabled = true
		return nil
	}
	value, err := decodeJSONString(content)
	if err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("connection override cannot be empty; omit it to inherit or use null to disable")
	}
	o.Value = value
	return nil
}

type extractionConnectionRequest struct {
	Cookie optionalConnectionString `json:"cookie"`
	Proxy  optionalConnectionString `json:"proxy"`
}

type extractionV1Request struct {
	URL        *string                      `json:"url"`
	Connection *extractionConnectionRequest `json:"connection,omitempty"`
}

type extractionConnectionResponse struct {
	CookieSource string `json:"cookie_source"`
	ProxySource  string `json:"proxy_source"`
}

type extractionWorkResponse struct {
	ID         int64  `json:"id"`
	PlatformID string `json:"platform_id"`
}

type extractionVersionResponse struct {
	ID         int64         `json:"id"`
	Number     int64         `json:"number"`
	CapturedAt time.Time     `json:"captured_at"`
	Resources  []apiResource `json:"resources"`
}

type extractionV1Response struct {
	RunID      int64                        `json:"run_id"`
	Source     string                       `json:"source"`
	Message    string                       `json:"message"`
	Connection extractionConnectionResponse `json:"connection"`
	Work       extractionWorkResponse       `json:"work"`
	Version    extractionVersionResponse    `json:"version"`
	Data       map[string]any               `json:"data"`
}

type accessResponse struct {
	Public        bool `json:"public"`
	Authenticated bool `json:"authenticated"`
	CanExtract    bool `json:"can_extract"`
}

type extractionOutcome struct {
	RunID   int64
	Source  string
	Version storedVersion
	Data    map[string]any
	Skipped bool
}

type extractionProblem struct {
	Status        int
	Code          string
	Message       string
	LegacyMessage string
	Err           error
}

func (e *extractionProblem) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func decodeJSONString(content []byte) (string, error) {
	var value string
	if err := decodeStrictJSON(bytes.NewReader(content), &value); err != nil {
		return "", errors.New("connection override must be a string or null")
	}
	return value, nil
}

func (a *App) handleAccess(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	settings, err := a.store.loadSettings(request.Context())
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SETTINGS_READ_FAILED", "无法读取访问设置")
		return
	}
	_, authenticated, err := a.authenticatedSession(request)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SESSION_CHECK_FAILED", "无法校验管理员会话")
		return
	}
	writeJSON(writer, http.StatusOK, accessResponse{
		Public: settings.Public, Authenticated: authenticated,
		CanExtract: settings.Public || authenticated,
	})
}

func (a *App) handleExtractionsV1(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	settings, authenticated, ok := a.authorizeExtraction(writer, request)
	if !ok {
		return
	}
	if !a.allowAnonymousExtraction(writer, request, authenticated) {
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, a.config.MaxBodyBytes)
	var input extractionV1Request
	if err := decodeStrictJSON(request.Body, &input); err != nil {
		writeAPIError(writer, requestDecodeStatus(err), "INVALID_REQUEST", "解析请求无效")
		return
	}
	if input.URL == nil || strings.TrimSpace(*input.URL) == "" {
		writeAPIError(writer, http.StatusUnprocessableEntity, "URL_REQUIRED", "url 为必填字段")
		return
	}
	requestedURL := strings.TrimSpace(*input.URL)
	if len(requestedURL) > maxRequestedURLBytes {
		writeAPIError(writer, http.StatusUnprocessableEntity, "URL_TOO_LONG", "url 长度超过限制")
		return
	}
	var cookieOverride, proxyOverride optionalConnectionString
	if input.Connection != nil {
		cookieOverride = input.Connection.Cookie
		proxyOverride = input.Connection.Proxy
	}
	if !allowConnectionOverrides(
		writer, request, authenticated, cookieOverride.Present || proxyOverride.Present,
	) {
		return
	}
	cookie, cookieSource, err := effectiveConnectionValue(settings.DefaultCookie, cookieOverride, true)
	if err != nil {
		writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_COOKIE", err.Error())
		return
	}
	proxy, proxySource, err := effectiveConnectionValue(settings.DefaultProxy, proxyOverride, false)
	if err != nil {
		writeAPIError(writer, http.StatusUnprocessableEntity, "INVALID_PROXY", err.Error())
		return
	}
	release, ok := a.beginAnonymousExtractionWork(writer, authenticated)
	if !ok {
		return
	}
	defer release()
	outcome, problem := a.performExtraction(
		request.Context(), requestedURL, cookie, proxy, cookieSource, proxySource, settings, false, nil,
	)
	release()
	if problem != nil {
		writeAPIError(writer, problem.Status, problem.Code, problem.Message)
		return
	}
	writeJSON(writer, http.StatusOK, extractionAPIResponse(outcome, cookieSource, proxySource))
}

func (a *App) authorizeExtraction(
	writer http.ResponseWriter,
	request *http.Request,
) (runtimeSettings, bool, bool) {
	settings, err := a.store.loadSettings(request.Context())
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SETTINGS_READ_FAILED", "无法读取访问设置")
		return runtimeSettings{}, false, false
	}
	_, authenticated, err := a.authenticatedSession(request)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "SESSION_CHECK_FAILED", "无法校验管理员会话")
		return runtimeSettings{}, false, false
	}
	if authenticated && !sameOriginRequest(request) {
		writeAPIError(writer, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "请求来源不受信任")
		return runtimeSettings{}, false, false
	}
	if !settings.Public {
		if !authenticated {
			writeAPIError(writer, http.StatusUnauthorized, "PUBLIC_ACCESS_DISABLED", "当前服务未开放匿名解析")
			return runtimeSettings{}, false, false
		}
	}
	return settings, authenticated, true
}

func allowConnectionOverrides(
	writer http.ResponseWriter,
	request *http.Request,
	authenticated, requested bool,
) bool {
	if !requested {
		return true
	}
	if !authenticated {
		writeAPIError(
			writer, http.StatusForbidden, "CONNECTION_OVERRIDE_FORBIDDEN",
			"匿名解析不能覆盖 Cookie 或代理，请先登录管理员账户",
		)
		return false
	}
	if !sameOriginRequest(request) {
		writeAPIError(
			writer, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "请求来源不受信任",
		)
		return false
	}
	return true
}

func effectiveConnectionValue(
	configured *string,
	override optionalConnectionString,
	cookie bool,
) (*string, string, error) {
	if !override.Present {
		if configured == nil || strings.TrimSpace(*configured) == "" {
			return nil, "none", nil
		}
		value := strings.TrimSpace(*configured)
		return &value, "default", nil
	}
	if override.Disabled {
		return nil, "disabled", nil
	}
	value := strings.TrimSpace(override.Value)
	if value == "" {
		return nil, "", errors.New("覆盖值不能为空")
	}
	if cookie {
		if len(value) > maxCookieBytes {
			return nil, "", fmt.Errorf("Cookie 不能超过 %d 字节", maxCookieBytes)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, "", errors.New("Cookie 不能包含换行符")
		}
	}
	return &value, "override", nil
}

func legacyConnectionValue(configured, override *string) (*string, string) {
	if override != nil && strings.TrimSpace(*override) != "" {
		value := strings.TrimSpace(*override)
		return &value, "override"
	}
	if configured != nil && strings.TrimSpace(*configured) != "" {
		value := strings.TrimSpace(*configured)
		return &value, "default"
	}
	return nil, "none"
}
func persistentCacheSources(cookieSource, proxySource string) bool {
	persistentSource := func(source string) bool {
		return source == "none" || source == "default"
	}
	return persistentSource(cookieSource) && persistentSource(proxySource)
}

func (a *App) performExtraction(
	ctx context.Context,
	requestedURL string,
	cookie, proxy *string,
	cookieSource, proxySource string,
	settings runtimeSettings,
	skipRecorded bool,
	persistenceIndexes []any,
) (extractionOutcome, *extractionProblem) {
	if len(requestedURL) == 0 || len(requestedURL) > maxRequestedURLBytes {
		return extractionOutcome{}, &extractionProblem{
			Status: http.StatusUnprocessableEntity, Code: "URL_TOO_LONG",
			Message: "url 长度超过限制", LegacyMessage: "请求参数无效",
			Err: errors.New("requested URL length is invalid"),
		}
	}
	runID, err := a.store.beginParseRun(ctx, historyRequestedURL(requestedURL), cookieSource, proxySource)
	if err != nil {
		return extractionOutcome{}, internalExtractionProblem("创建解析记录失败", err)
	}
	fail := func(problem *extractionProblem) (extractionOutcome, *extractionProblem) {
		recordContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := a.store.failParseRun(recordContext, runID, problem.Code); err != nil {
			a.logger.Printf("record failed extraction %d: %v", runID, err)
		}
		return extractionOutcome{}, problem
	}

	client, err := a.clientFactory(proxy)
	if err != nil {
		return fail(&extractionProblem{
			Status: http.StatusUnprocessableEntity, Code: "INVALID_PROXY",
			Message: "代理参数无效", LegacyMessage: "代理参数无效", Err: err,
		})
	}
	if proxy != nil {
		defer client.CloseIdleConnections()
	}
	headers := requestHeaders(cookie)
	cacheable := persistentCacheSources(cookieSource, proxySource)

	resolveContext, cancelResolve := context.WithTimeout(ctx, a.config.RequestTimeout)
	resolved, err := resolveLink(resolveContext, requestedURL, client, headers)
	cancelResolve()
	if err != nil {
		return fail(&extractionProblem{
			Status: http.StatusUnprocessableEntity, Code: "UNSUPPORTED_LINK",
			Message: "无法识别作品链接", LegacyMessage: "提取小红书作品链接失败", Err: err,
		})
	}
	cacheScope := a.store.secrets.cacheScope(cookie, proxy, resolved.URL)

	unlock, err := a.downloads.lockWork(ctx, resolved.NoteID)
	if err != nil {
		return fail(internalExtractionProblem("等待作品处理锁失败", err))
	}
	defer unlock()

	if skipRecorded {
		recorded := a.records.Has(resolved.NoteID)
		if !recorded {
			recorded, err = a.store.hasLegacyDownload(ctx, resolved.NoteID)
			if err != nil {
				return fail(internalExtractionProblem("读取旧下载记录失败", err))
			}
		}
		if recorded {
			if err := a.store.completeSkippedRun(ctx, runID, resolved.NoteID); err != nil {
				return fail(internalExtractionProblem("更新解析记录失败", err))
			}
			return extractionOutcome{RunID: runID, Source: "skipped", Skipped: true}, nil
		}
	}

	if !settings.Refetch && cacheable {
		cached, err := a.store.latestVersion(ctx, resolved.NoteID, cacheScope)
		if err == nil {
			if err := a.store.completeCachedRun(ctx, runID, cached); err != nil {
				return fail(internalExtractionProblem("更新缓存解析记录失败", err))
			}
			cached.Resources = a.persistResources(ctx, client, cached, settings, persistenceIndexes)
			return extractionOutcome{
				RunID: runID, Source: "cache", Version: cached, Data: cached.Data,
			}, nil
		}
		if !errors.Is(err, errNoCachedVersion) {
			return fail(internalExtractionProblem("读取缓存版本失败", err))
		}
	}

	fetchContext, cancelFetch := context.WithTimeout(ctx, a.config.RequestTimeout)
	html, err := fetchPage(fetchContext, client, resolved.URL, headers, a.config.MaxUpstreamBody)
	cancelFetch()
	if err != nil {
		status := http.StatusBadGateway
		code := "UPSTREAM_FETCH_FAILED"
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			code = "UPSTREAM_TIMEOUT"
		}
		return fail(&extractionProblem{
			Status: status, Code: code, Message: "获取作品页面失败",
			LegacyMessage: "获取小红书作品数据失败", Err: err,
		})
	}
	note, err := parseInitialState(html, resolved.NoteID)
	if err != nil {
		return fail(&extractionProblem{
			Status: http.StatusBadGateway, Code: "UPSTREAM_PARSE_FAILED",
			Message: "解析作品页面失败", LegacyMessage: "获取小红书作品数据失败", Err: err,
		})
	}
	data, err := extractWork(note, canonicalWorkURL(resolved.NoteID), resolved.NoteID)
	if err != nil {
		return fail(&extractionProblem{
			Status: http.StatusBadGateway, Code: "WORK_EXTRACT_FAILED",
			Message: "提取作品数据失败", LegacyMessage: "获取小红书作品数据失败", Err: err,
		})
	}
	version, err := a.store.persistFetchedVersion(
		ctx, runID, resolved.NoteID, cacheScope, cacheable, data,
	)
	if err != nil {
		return fail(internalExtractionProblem("保存作品版本失败", err))
	}
	version.Resources = a.persistResources(ctx, client, version, settings, persistenceIndexes)
	return extractionOutcome{
		RunID: runID, Source: "fetched", Version: version, Data: data,
	}, nil
}

func (a *App) persistResources(
	ctx context.Context,
	client *http.Client,
	version storedVersion,
	settings runtimeSettings,
	indexes []any,
) []storedResource {
	persistenceData, disabledResults := persistenceDataForIndexes(version.Data, indexes)
	results := persistVersionResources(
		ctx, client, a.config.VolumeDir, version.PlatformID, version.VersionNumber,
		persistenceData,
		mediaPersistencePolicy{
			Text: settings.SaveText, Images: settings.SaveImages, Videos: settings.SaveVideos,
		},
		a.downloads,
	)
	for _, result := range results {
		if result.cause != nil {
			a.logger.Printf("persist version %d %s/%d: %v", version.ID, result.Kind, result.Ordinal, result.cause)
		}
	}

	results = append(results, disabledResults...)
	resources, err := a.store.updateVersionResources(ctx, version.ID, results)
	if err != nil {
		a.logger.Printf("record version %d resources: %v", version.ID, err)
		return version.Resources
	}
	for _, result := range results {
		if result.Status == "stored" && result.Kind != "text" {
			if err := a.records.Add(version.PlatformID); err != nil {
				a.logger.Printf("record legacy download %s: %v", version.PlatformID, err)
			}
			if err := a.store.markLegacyDownload(ctx, version.PlatformID, "sqlite", time.Now()); err != nil {
				a.logger.Printf("record sqlite download %s: %v", version.PlatformID, err)
			}
			break
		}
	}
	return resources
}

func canonicalWorkURL(noteID string) string {
	return "https://www.xiaohongshu.com/explore/" + url.PathEscape(noteID)
}

func persistenceDataForIndexes(data map[string]any, indexes []any) (map[string]any, []mediaPersistenceResult) {
	selected, err := selectedIndexes(indexes)
	if err != nil || selected == nil || stringValue(data["作品类型"]) == "视频" {
		return data, nil
	}
	urls, lives, err := mediaURLs(data)
	if err != nil {
		return data, nil
	}
	filteredURLs := append([]string(nil), urls...)
	filteredLives := append([]any(nil), lives...)
	disabled := make([]mediaPersistenceResult, 0)
	for index := range filteredURLs {
		ordinal := index + 1
		if _, ok := selected[ordinal]; ok {
			continue
		}
		if strings.TrimSpace(filteredURLs[index]) != "" {
			disabled = append(disabled, mediaPersistenceResult{
				Kind: "image", Ordinal: ordinal, RemoteURL: filteredURLs[index], Status: "disabled",
			})
			filteredURLs[index] = ""
		}
		if index < len(filteredLives) {
			if liveURL := firstString(filteredLives[index]); liveURL != "" {
				disabled = append(disabled, mediaPersistenceResult{
					Kind: "video", Ordinal: ordinal, RemoteURL: liveURL, Status: "disabled",
				})
				filteredLives[index] = nil
			}
		}
	}
	copyData := make(map[string]any, len(data))
	for key, value := range data {
		copyData[key] = value
	}
	copyData["下载地址"] = filteredURLs
	copyData["动图地址"] = filteredLives
	return copyData, disabled
}

func internalExtractionProblem(message string, err error) *extractionProblem {
	return &extractionProblem{
		Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR",
		Message: message, LegacyMessage: "获取小红书作品数据失败", Err: err,
	}
}

func extractionAPIResponse(
	outcome extractionOutcome,
	cookieSource, proxySource string,
) extractionV1Response {
	return extractionV1Response{
		RunID:   outcome.RunID,
		Source:  outcome.Source,
		Message: "获取小红书作品数据成功",
		Connection: extractionConnectionResponse{
			CookieSource: cookieSource, ProxySource: proxySource,
		},
		Work: extractionWorkResponse{
			ID: outcome.Version.WorkID, PlatformID: outcome.Version.PlatformID,
		},
		Version: extractionVersionResponse{
			ID: outcome.Version.ID, Number: outcome.Version.VersionNumber,
			CapturedAt: outcome.Version.CapturedAt,
			Resources:  publicResources(outcome.Version.Resources),
		},
		Data: outcome.Data,
	}
}

func (a *App) handleLegacyExtraction(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	settings, authenticated, ok := a.authorizeExtraction(writer, request)
	if !ok {
		return
	}
	if !a.allowAnonymousExtraction(writer, request, authenticated) {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, a.config.MaxBodyBytes)
	params, err := decodeExtractRequest(request.Body)
	if err != nil {
		status := http.StatusUnprocessableEntity
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(writer, status, map[string]any{
			"message": "请求参数无效", "detail": err.Error(),
		})
		return
	}
	params.URL = strings.TrimSpace(params.URL)
	if len(params.URL) > maxRequestedURLBytes {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]any{
			"message": "请求参数无效", "detail": "url 长度超过限制",
		})
		return
	}
	connectionOverride :=
		(params.Cookie != nil && strings.TrimSpace(*params.Cookie) != "") ||
			(params.Proxy != nil && strings.TrimSpace(*params.Proxy) != "")
	if !allowConnectionOverrides(writer, request, authenticated, connectionOverride) {
		return
	}
	cookie, cookieSource := legacyConnectionValue(settings.DefaultCookie, params.Cookie)
	proxy, proxySource := legacyConnectionValue(settings.DefaultProxy, params.Proxy)
	legacySettings := settings
	legacySettings.SaveText = settings.SaveText && params.Download
	legacySettings.SaveImages = settings.SaveImages && params.Download
	legacySettings.SaveVideos = settings.SaveVideos && params.Download
	var persistenceIndexes []any
	if params.Download {
		persistenceIndexes = params.Index
	}
	release, ok := a.beginAnonymousExtractionWork(writer, authenticated)
	if !ok {
		return
	}
	defer release()
	outcome, problem := a.performExtraction(
		request.Context(), params.URL, cookie, proxy, cookieSource, proxySource,
		legacySettings, params.Skip, persistenceIndexes,
	)
	release()
	sanitized := params
	sanitized.Cookie = nil
	sanitized.Proxy = nil
	if problem != nil {
		if problem.Code == "INVALID_PROXY" {
			writeJSON(writer, http.StatusUnprocessableEntity, map[string]any{
				"message": "代理参数无效", "detail": "代理地址格式或网络目标不受支持",
			})
			return
		}
		if problem.Status == http.StatusInternalServerError {
			writeJSON(writer, problem.Status, map[string]string{"message": problem.LegacyMessage})
			return
		}
		a.writeExtract(writer, problem.LegacyMessage, sanitized, nil)
		return
	}
	if outcome.Skipped {
		a.writeExtract(writer, "获取小红书作品数据成功", sanitized, map[string]any{
			"message": "作品存在下载记录，跳过处理",
		})
		return
	}
	a.writeExtract(writer, "获取小红书作品数据成功", sanitized, legacyResponseData(outcome.Data, outcome.Version.Resources, params.Download))
}
func historyRequestedURL(raw string) string {
	candidate, _ := firstSupportedCandidate(raw)
	if candidate == "" {
		return raw
	}
	candidate = ensureHTTPS(candidate)
	parsed, err := url.Parse(candidate)
	if err != nil {
		if index := strings.IndexAny(candidate, "?#"); index >= 0 {
			candidate = candidate[:index]
		}
		return candidate
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" && !(parsed.Scheme == "https" && port == "443") {
		host += ":" + port
	}
	parsed.Host = host
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func legacyResponseData(data map[string]any, resources []storedResource, download bool) map[string]any {
	if !download {
		return data
	}
	seen := make(map[string]struct{})
	codes := make([]string, 0)
	for _, resource := range resources {
		if resource.SaveStatus != "failed" {
			continue
		}
		code := stableSaveErrorCode("failed", resource.SaveError)
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return data
	}
	result := make(map[string]any, len(data)+1)
	for key, value := range data {
		result[key] = value
	}
	result["下载错误"] = strings.Join(codes, ",")
	return result
}
