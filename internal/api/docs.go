package api

func openAPIDocument() map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "XHS-Downloader",
			"version":     "2.8",
			"description": "Go 核心 API：提取小红书 / RedNote 作品信息、媒体地址并可选下载。",
		},
		"paths": map[string]any{
			"/healthz": map[string]any{
				"get": map[string]any{
					"summary":   "服务健康检查",
					"responses": map[string]any{"200": map[string]any{"description": "OK"}},
				},
			},
			"/xhs/detail": map[string]any{
				"post": map[string]any{
					"summary": "获取作品数据及下载地址",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ExtractParams"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "逻辑处理结果",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/ExtractResponse"},
								},
							},
						},
						"422": map[string]any{"description": "请求参数无效"},
						"413": map[string]any{"description": "请求体超过大小限制"},
					},
				},
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"ExtractParams": map[string]any{
					"type":     "object",
					"required": []string{"url"},
					"properties": map[string]any{
						"url":      map[string]any{"type": "string"},
						"download": map[string]any{"type": "boolean", "default": false},
						"index": map[string]any{
							"type": []string{"array", "null"},
							"items": map[string]any{"oneOf": []any{
								map[string]any{"type": "integer", "minimum": 1},
								map[string]any{"type": "string", "pattern": `^\s*[1-9][0-9]*\s*$`},
							}},
							"default": nil,
						},
						"cookie": map[string]any{"type": []string{"string", "null"}, "default": nil},
						"proxy":  map[string]any{"type": []string{"string", "null"}, "default": nil},
						"skip":   map[string]any{"type": "boolean", "default": false},
					},
				},
				"ExtractResponse": map[string]any{
					"type":     "object",
					"required": []string{"message", "params", "data"},
					"properties": map[string]any{
						"message": map[string]any{"type": "string"},
						"params":  map[string]any{"$ref": "#/components/schemas/ExtractParams"},
						"data":    map[string]any{"type": []string{"object", "null"}, "additionalProperties": true},
					},
				},
			},
		},
	}
}

const docsHTML = "<!doctype html><html lang='zh-CN'><head><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>XHS-Downloader API</title><style>body{max-width:880px;margin:0 auto;padding:48px 24px;font:16px/1.65 system-ui;color:#18181b;background:#fafafa}h1{font-size:38px;letter-spacing:-.03em}section{background:#fff;border:1px solid #e4e4e7;border-radius:18px;padding:24px;margin:20px 0}code,pre{font-family:ui-monospace,monospace}pre{overflow:auto;background:#18181b;color:#f4f4f5;padding:18px;border-radius:12px}a{color:#e11d48}</style></head><body><h1>XHS-Downloader Go API</h1><p>核心 REST API 与 React Web 控制台由同一 Go 服务提供。</p><section><h2>POST /xhs/detail</h2><p>请求 JSON 字段：url（必填）、download、index、cookie、proxy、skip。</p><pre>curl -X POST http://127.0.0.1:5556/xhs/detail -H 'Content-Type: application/json' -d '{&quot;url&quot;:&quot;https://www.xiaohongshu.com/explore/作品ID&quot;,&quot;download&quot;:false}'</pre></section><section><h2>接口定义</h2><p><a href='/openapi.json'>OpenAPI 3.1 JSON</a> · <a href='/healthz'>健康检查</a> · <a href='/'>Web 控制台</a></p></section></body></html>"
