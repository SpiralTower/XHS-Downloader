package api

func openAPIDocument() map[string]any {
	jsonContent := func(schema map[string]any) map[string]any {
		return map[string]any{"application/json": map[string]any{"schema": schema}}
	}
	response := func(description string) map[string]any {
		return map[string]any{"description": description}
	}
	jsonResponse := func(description string, schema map[string]any) map[string]any {
		return map[string]any{"description": description, "content": jsonContent(schema)}
	}
	requestBody := func(reference string) map[string]any {
		return map[string]any{
			"required": true,
			"content":  jsonContent(map[string]any{"$ref": reference}),
		}
	}
	adminSecurity := []any{map[string]any{"cookieAuth": []any{}}}
	publicOrAdminSecurity := []any{map[string]any{}, map[string]any{"cookieAuth": []any{}}}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "XHS-Downloader",
			"version":     "3.0",
			"description": "Go 核心 API、管理员会话、运行时策略与 SQLite 版本历史。Cookie、代理凭据和授权查询参数不会明文持久化；Cookie、代理凭据与服务端文件路径不会出现在响应中；资源 save_error 仅返回稳定错误码。",
		},
		"paths": map[string]any{
			"/healthz": map[string]any{
				"get": map[string]any{
					"summary":   "服务与数据库健康检查",
					"responses": map[string]any{"200": response("服务与 SQLite 可用"), "503": jsonResponse("SQLite 不可用（DATABASE_UNAVAILABLE）", map[string]any{"$ref": "#/components/schemas/APIError"})},
				},
			},
			"/api/v1/access": map[string]any{
				"get": map[string]any{
					"summary": "读取非敏感访问状态",
					"responses": map[string]any{
						"200": jsonResponse("访问状态", map[string]any{"$ref": "#/components/schemas/AccessStatus"}),
					},
				},
			},
			"/api/v1/extractions": map[string]any{
				"post": map[string]any{
					"summary":     "解析作品并按管理策略记录版本和资源",
					"security":    publicOrAdminSecurity,
					"requestBody": requestBody("#/components/schemas/ExtractionRequest"),
					"responses": map[string]any{
						"200": jsonResponse("解析结果", map[string]any{"$ref": "#/components/schemas/ExtractionResponse"}),
						"401": jsonResponse("未开放匿名解析或需要登录", map[string]any{"$ref": "#/components/schemas/APIError"}),
						"422": jsonResponse("请求参数无效", map[string]any{"$ref": "#/components/schemas/APIError"}),
						"502": jsonResponse("上游获取或解析失败", map[string]any{"$ref": "#/components/schemas/APIError"}),
					},
				},
			},
			"/api/v1/popular-works": map[string]any{
				"get": map[string]any{
					"summary":  "读取首页累计、近 30 天与近 7 天热门作品",
					"security": publicOrAdminSecurity,
					"responses": map[string]any{
						"200": jsonResponse("榜单开关与 Top10 数据", map[string]any{"$ref": "#/components/schemas/PopularWorks"}),
						"401": jsonResponse("未开放匿名访问", map[string]any{"$ref": "#/components/schemas/APIError"}),
					},
				},
			},
			"/api/admin/v1/auth/session": map[string]any{
				"get": map[string]any{
					"summary": "读取管理会话状态",
					"responses": map[string]any{
						"200": jsonResponse("会话状态", map[string]any{"$ref": "#/components/schemas/AdminSession"}),
					},
				},
				"post": map[string]any{
					"summary":     "登录管理端",
					"requestBody": requestBody("#/components/schemas/AdminLogin"),
					"responses": map[string]any{
						"200": jsonResponse("登录成功", map[string]any{"$ref": "#/components/schemas/AdminSession"}),
						"401": jsonResponse("用户名或密码错误", map[string]any{"$ref": "#/components/schemas/APIError"}),
						"429": jsonResponse("登录尝试过多", map[string]any{"$ref": "#/components/schemas/APIError"}),
					},
				},
				"delete": map[string]any{
					"summary":   "注销当前管理会话",
					"security":  adminSecurity,
					"responses": map[string]any{"204": response("已注销"), "401": response("需要登录")},
				},
			},
			"/api/admin/v1/settings": map[string]any{
				"get": map[string]any{
					"summary":  "读取脱敏后的运行时设置",
					"security": adminSecurity,
					"responses": map[string]any{
						"200": jsonResponse("设置", map[string]any{"$ref": "#/components/schemas/AdminSettings"}),
					},
				},
				"patch": map[string]any{
					"summary":     "按 revision 更新运行时设置",
					"security":    adminSecurity,
					"requestBody": requestBody("#/components/schemas/AdminSettingsPatch"),
					"responses": map[string]any{
						"200": jsonResponse("更新后的设置", map[string]any{"$ref": "#/components/schemas/AdminSettings"}),
						"409": jsonResponse("revision 冲突", map[string]any{"$ref": "#/components/schemas/APIError"}),
					},
				},
			},
			"/api/admin/v1/history": map[string]any{
				"get": map[string]any{
					"summary":  "分页读取每次解析记录",
					"security": adminSecurity,
					"parameters": []any{
						map[string]any{"name": "cursor", "in": "query", "schema": map[string]any{"type": "string"}},
						map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 50}},
					},
					"responses": map[string]any{"200": response("解析记录和 next_cursor")},
				},
			},
			"/api/admin/v1/works": map[string]any{
				"get": map[string]any{
					"summary":  "分页读取唯一作品、解析次数和已保存预览",
					"security": adminSecurity,
					"parameters": []any{
						map[string]any{"name": "cursor", "in": "query", "schema": map[string]any{"type": "string"}},
						map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 25}},
					},
					"responses": map[string]any{"200": jsonResponse("作品分页", map[string]any{"$ref": "#/components/schemas/AdminWorkPage"})},
				},
			},
			"/api/admin/v1/works/{id}": map[string]any{
				"get": map[string]any{
					"summary":  "读取作品的全部版本和版本资源",
					"security": adminSecurity,
					"parameters": []any{
						map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "integer", "minimum": 1}},
					},
					"responses": map[string]any{"200": response("作品、版本与资源"), "404": response("作品不存在")},
				},
			},
			"/api/admin/v1/resources/{id}/content": map[string]any{
				"get": map[string]any{
					"summary":  "读取已保存的作品缩略图",
					"security": adminSecurity,
					"parameters": []any{
						map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "integer", "minimum": 1}},
					},
					"responses": map[string]any{"200": response("已保存图片内容"), "404": response("资源不存在")},
				},
			},
			"/xhs/detail": map[string]any{
				"post": map[string]any{
					"summary":     "旧版兼容解析入口",
					"description": "已弃用。只有 download=true 且管理员开启对应文案/图片/视频类别时才保存；download=false 仅解析。index 为 1 起始并限制图文/动图保存项。保存失败时 data.下载错误仅包含本次失败资源的稳定错误码；Cookie 和代理始终脱敏。新客户端应使用 /api/v1/extractions。",
					"deprecated":  true,
					"security":    publicOrAdminSecurity,
					"requestBody": requestBody("#/components/schemas/LegacyExtractParams"),
					"responses": map[string]any{
						"200": jsonResponse("兼容响应", map[string]any{"$ref": "#/components/schemas/LegacyExtractResponse"}),
						"401": response("未开放匿名解析"),
						"422": response("请求参数无效"),
					},
				},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"cookieAuth": map[string]any{"type": "apiKey", "in": "cookie", "name": adminSessionCookie},
			},
			"schemas": map[string]any{
				"APIError": map[string]any{
					"type": "object", "required": []string{"code", "message"},
					"properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}},
				},
				"AccessStatus": map[string]any{
					"type": "object", "required": []string{"public", "authenticated", "can_extract"},
					"properties": map[string]any{
						"public": map[string]any{"type": "boolean"}, "authenticated": map[string]any{"type": "boolean"}, "can_extract": map[string]any{"type": "boolean"},
					},
				},
				"ConnectionOverrides": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cookie": map[string]any{"type": []string{"string", "null"}, "writeOnly": true, "description": "省略继承默认值；null 禁用默认值；字符串仅覆盖本次请求。任何显式覆盖（含 null）都不使用持久缓存。"},
						"proxy":  map[string]any{"type": []string{"string", "null"}, "writeOnly": true, "description": "省略继承默认值；显式覆盖仅用于本次请求且不使用持久缓存。"},
					},
				},
				"ExtractionRequest": map[string]any{
					"type": "object", "required": []string{"url"}, "additionalProperties": false,
					"properties": map[string]any{
						"url":        map[string]any{"type": "string", "maxLength": maxRequestedURLBytes},
						"connection": map[string]any{"$ref": "#/components/schemas/ConnectionOverrides"},
					},
				},
				"ExtractionResponse": map[string]any{
					"type": "object", "required": []string{"run_id", "source", "connection", "work", "version", "data"},
					"properties": map[string]any{
						"run_id": map[string]any{"type": "integer"}, "source": map[string]any{"enum": []string{"fetched", "cache"}},
						"connection": map[string]any{"type": "object", "description": "仅包含 default/override/disabled/none 来源标记，不包含凭据。"},
						"work":       map[string]any{"type": "object"}, "version": map[string]any{"type": "object"}, "data": map[string]any{"type": "object", "additionalProperties": true},
					},
				},
				"AdminLogin": map[string]any{
					"type": "object", "required": []string{"username", "password"}, "additionalProperties": false,
					"properties": map[string]any{"username": map[string]any{"type": "string"}, "password": map[string]any{"type": "string", "writeOnly": true}},
				},
				"AdminSession": map[string]any{
					"type": "object", "required": []string{"authenticated"},
					"properties": map[string]any{"authenticated": map[string]any{"type": "boolean"}, "username": map[string]any{"type": "string"}, "expires_at": map[string]any{"type": "string", "format": "date-time"}},
				},
				"SecretView": map[string]any{
					"type": "object", "required": []string{"configured"},
					"properties": map[string]any{"configured": map[string]any{"type": "boolean"}, "display": map[string]any{"type": "string", "description": "不含用户名、密码、路径、查询参数或片段。"}},
				},
				"AdminSettings": map[string]any{
					"type": "object", "required": []string{"revision", "public", "show_popular", "save", "refetch", "default_cookie", "default_proxy"},
					"properties": map[string]any{
						"revision": map[string]any{"type": "integer"}, "public": map[string]any{"type": "boolean"}, "show_popular": map[string]any{"type": "boolean"}, "save": map[string]any{"type": "object"}, "refetch": map[string]any{"type": "boolean"},
						"default_cookie": map[string]any{"$ref": "#/components/schemas/SecretView"}, "default_proxy": map[string]any{"$ref": "#/components/schemas/SecretView"},
					},
				},
				"SecretPatch": map[string]any{
					"type": "object", "required": []string{"action"},
					"properties": map[string]any{"action": map[string]any{"enum": []string{"keep", "replace", "clear"}}, "value": map[string]any{"type": "string", "writeOnly": true}},
				},
				"AdminSettingsPatch": map[string]any{
					"type": "object", "required": []string{"revision"}, "additionalProperties": false,
					"properties": map[string]any{
						"revision": map[string]any{"type": "integer", "minimum": 1}, "public": map[string]any{"type": "boolean"}, "show_popular": map[string]any{"type": "boolean"}, "save": map[string]any{"type": "object"}, "refetch": map[string]any{"type": "boolean"},
						"default_cookie": map[string]any{"$ref": "#/components/schemas/SecretPatch"}, "default_proxy": map[string]any{"$ref": "#/components/schemas/SecretPatch"},
					},
				},
				"PopularWork": map[string]any{
					"type": "object", "required": []string{"platform_id", "work_url", "parse_count"},
					"properties": map[string]any{
						"platform_id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "work_url": map[string]any{"type": "string", "format": "uri"}, "parse_count": map[string]any{"type": "integer", "minimum": 1},
					},
				},
				"PopularWorks": map[string]any{
					"type": "object", "required": []string{"enabled", "all_time", "recent_30d", "recent_7d"},
					"properties": map[string]any{
						"enabled":    map[string]any{"type": "boolean"},
						"all_time":   map[string]any{"type": "array", "maxItems": 10, "items": map[string]any{"$ref": "#/components/schemas/PopularWork"}},
						"recent_30d": map[string]any{"type": "array", "maxItems": 10, "items": map[string]any{"$ref": "#/components/schemas/PopularWork"}},
						"recent_7d":  map[string]any{"type": "array", "maxItems": 10, "items": map[string]any{"$ref": "#/components/schemas/PopularWork"}},
					},
				},
				"AdminWorkListItem": map[string]any{
					"type": "object", "required": []string{"id", "platform_id", "parse_count", "version_count"},
					"properties": map[string]any{
						"id": map[string]any{"type": "integer"}, "platform_id": map[string]any{"type": "string"},
						"parse_count": map[string]any{"type": "integer", "minimum": 0}, "version_count": map[string]any{"type": "integer", "minimum": 0},
						"last_parsed_at": map[string]any{"type": "string", "format": "date-time"}, "title": map[string]any{"type": "string"}, "thumbnail_url": map[string]any{"type": "string"},
					},
				},
				"AdminWorkPage": map[string]any{
					"type": "object", "required": []string{"items", "next_cursor"},
					"properties": map[string]any{
						"items":       map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/AdminWorkListItem"}},
						"next_cursor": map[string]any{"type": []string{"string", "null"}},
					},
				},
				"LegacyExtractParams": map[string]any{
					"type": "object", "required": []string{"url"},
					"properties": map[string]any{
						"url": map[string]any{"type": "string", "maxLength": maxRequestedURLBytes}, "download": map[string]any{"type": "boolean", "default": false, "description": "仅 true 时允许按管理员开启的类别保存；false 不保存。"},
						"index":  map[string]any{"type": []string{"array", "null"}, "description": "download=true 时按 1 起始选择图文图片及同序号动图；不影响视频作品。", "items": map[string]any{"oneOf": []any{map[string]any{"type": "integer", "minimum": 1}, map[string]any{"type": "string", "pattern": "^[1-9][0-9]*$"}}}},
						"cookie": map[string]any{"type": []string{"string", "null"}, "writeOnly": true}, "proxy": map[string]any{"type": []string{"string", "null"}, "writeOnly": true}, "skip": map[string]any{"type": "boolean"},
					},
				},
				"LegacyExtractResponse": map[string]any{
					"type": "object", "required": []string{"message", "params", "data"},
					"properties": map[string]any{
						"message": map[string]any{"type": "string"}, "params": map[string]any{"$ref": "#/components/schemas/LegacyExtractParams"},
						"data": map[string]any{"type": []string{"object", "null"}, "description": "download=true 且资源保存失败时可含 下载错误；值为去重、稳定顺序的稳定错误码，不含原始网络或文件错误。", "additionalProperties": true},
					},
				},
			},
		},
	}
}

const docsHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>XHS-Downloader API</title><style>
body{max-width:920px;margin:0 auto;padding:48px 24px;font:16px/1.65 system-ui;color:#18181b;background:#fafafa}h1{font-size:38px;letter-spacing:-.03em}section{background:#fff;border:1px solid #e4e4e7;border-radius:18px;padding:24px;margin:20px 0}code,pre{font-family:ui-monospace,monospace}pre{overflow:auto;background:#18181b;color:#f4f4f5;padding:18px;border-radius:12px}a{color:#e11d48}
</style></head><body><h1>XHS-Downloader Go API</h1>
<p>用户端、管理端与 SQLite 版本历史由同一 Go 服务提供。管理会话使用同源 HttpOnly Cookie。</p>
<section><h2>用户解析</h2><p><code>POST /api/v1/extractions</code> 只接受作品链接和可选的本次 Cookie / 代理覆盖值；保存与重新抓取策略由管理端控制。</p>
<pre>curl -X POST http://127.0.0.1:5556/api/v1/extractions \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://www.xiaohongshu.com/explore/作品ID"}'</pre></section>
<section><h2>管理端</h2><p><code>/admin/login</code> 登录后可管理默认连接、公共访问、首页热门榜单、文案/图片/视频保存策略，并查看作品统计、缩略图和版本历史。</p>
<p>环境变量 <code>XHS_ADMIN_PASSWORD</code> 或 <code>XHS_ADMIN_PASSWORD_FILE</code> 必须在生产启动时显式配置；<code>XHS_MAX_MEDIA_BYTES</code> 限制单个媒体资源大小。</p>
<p>旧接口 <code>/xhs/detail</code> 只有在 <code>download=true</code> 且管理员开启对应类别时保存；失败仅返回稳定错误码。</p></section>
<section><h2>接口定义</h2><p><a href="/openapi.json">OpenAPI 3.1 JSON</a> · <a href="/healthz">健康检查</a> · <a href="/">用户端</a> · <a href="/admin/login">管理端</a></p></section>
</body></html>`
