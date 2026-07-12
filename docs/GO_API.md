# Go 核心 API 与 React Web 控制台

本仓库的核心 REST API 已重构为 Go，Web 控制台使用 React 19、HeroUI v3、Tailwind CSS v4 和 Vite 8。原 Python 桌面版、TUI、CLI 与 MCP 代码仍保留，不属于本次重构范围。

## 工具链

- Go 1.23 或更高版本（容器构建使用 Go 1.24）
- Node.js 22 与 npm 10
- Python 3.12 或更高版本，仅用于保留的 Python 桌面功能
- Docker 26 或兼容版本

CNB 云原生开发会通过 [`.ide/Dockerfile`](../.ide/Dockerfile) 提供 Go 1.24、Python 3.12 和 Node.js 22。

## 本地开发

安装前端依赖并构建：

~~~bash
npm --prefix web ci
npm --prefix web run build
~~~

启动 Go 服务：

~~~bash
go run ./cmd/api
~~~

访问：

- Web 控制台：<http://127.0.0.1:5556/>
- API 文档：<http://127.0.0.1:5556/docs>
- OpenAPI：<http://127.0.0.1:5556/openapi.json>
- 健康检查：<http://127.0.0.1:5556/healthz>

前端热更新开发：

~~~bash
npm --prefix web run dev
~~~

Vite 在 5173 端口启动，并把 /xhs 与 /healthz 代理到 http://localhost:5556。

## REST API

核心接口保持为：

~~~text
POST /xhs/detail
Content-Type: application/json
~~~

请求示例：

~~~json
{
  "url": "https://www.xiaohongshu.com/explore/作品ID",
  "download": false,
  "index": null,
  "cookie": null,
  "proxy": null,
  "skip": false
}
~~~

响应保持 {message, params, data} 结构。支持小红书与 RedNote 的 explore、discovery/item、user/profile/.../作品ID 链接，以及 xhslink.com 短链。

说明：

- download=true 时，媒体保存到 XHS_VOLUME_DIR/Download。
- index 仅接受从 1 开始的整数或整数字符串；为空时下载全部。
- skip=true 时，已成功下载并记录的作品会被跳过。
- cookie 与 proxy 仅用于当前请求；Web 控制台不会把它们写入浏览器持久存储。
- proxy 支持 http:// 与 https:// URL；默认只允许解析到公网 IP 的代理。
- 仅在受信本地环境需要连接私网代理时，显式设置 XHS_ALLOW_PRIVATE_PROXY=true；不要在公开服务中开启。
- 请求体超过 XHS_MAX_BODY_BYTES 时返回 HTTP 413；其他参数校验错误返回 HTTP 422。
- download=true 但媒体保存失败时，作品数据仍会返回，并在 data.下载错误 中说明原因。

## 运行配置

| 环境变量 | 默认值 | 说明 |
|---|---:|---|
| HOST | 0.0.0.0 | 监听地址 |
| PORT | 5556 | HTTP 端口 |
| WEB_DIST_DIR | web/dist | Vite 构建产物目录 |
| XHS_VOLUME_DIR | Volume | 下载与记录持久化目录 |
| XHS_REQUEST_TIMEOUT | 15s | 短链解析和作品页面抓取的单阶段超时；不限制媒体流总下载时长 |
| XHS_DOWNLOAD_TIMEOUT | 30m | 单个媒体任务取得下载槽后的总超时 |
| XHS_DOWNLOAD_IDLE_TIMEOUT | 60s | 媒体响应体连续无读取进度的超时 |
| XHS_ALLOW_PRIVATE_PROXY | false | 是否允许代理解析到私网、回环或链路本地地址；仅用于受信环境 |
| XHS_MAX_BODY_BYTES | 1048576 | API 请求体上限 |

## Docker

推荐使用 Compose：

~~~bash
docker compose up --build -d
docker compose ps
~~~

或者直接运行：

~~~bash
docker build -t xhs-downloader:local .
docker run --rm -p 5556:5556 \
  -v xhs-volume:/app/Volume \
  xhs-downloader:local
~~~

最终镜像以 UID/GID 10001 的非 root 用户运行，内置健康检查，并把 /app/Volume 声明为持久卷。Compose 使用固定名称 xhs-volume，避免项目目录名变化时创建不同的卷。

从旧版本升级时，已有卷可能由 root 创建，导致 UID 10001 无法写入。停止服务后可修复卷的属主：

~~~bash
docker compose down
docker run --rm --user 0 \
  -v xhs-volume:/app/Volume \
  alpine:3.22 chown -R 10001:10001 /app/Volume
docker compose up --build -d
~~~

旧版 Compose 创建的卷名可能带项目名前缀，例如 myproject_xhs-volume。先用 docker volume ls 确认实际名称，再把旧卷只读挂载并复制到固定的 xhs-volume：

~~~bash
docker compose down
docker volume create xhs-volume
docker run --rm --user 0 \
  -v myproject_xhs-volume:/from:ro \
  -v xhs-volume:/to \
  alpine:3.22 sh -c 'cp -a /from/. /to/ && chown -R 10001:10001 /to'
docker compose up --build -d
~~~

请把 myproject_xhs-volume 替换为 docker volume ls 显示的旧卷名。宿主机目录绑定挂载也必须确保 UID/GID 10001 对该目录具有读写权限。

## CNB 构建

[`.cnb.yml`](../.cnb.yml) 包含：

- CNB WebIDE 开发环境：Go 1.24、Python 3.12、Node.js 22 与 Docker 服务。
- 所有分支/PR：运行 go test ./...、go vet ./...、前端可复现构建，并使用生产 Dockerfile 构建镜像。
- master 分支 push：测试通过后构建镜像，并推送 ${CNB_DOCKER_REGISTRY}/${CNB_REPO_SLUG_LOWERCASE} 的提交短 SHA 与 latest 标签。

## 验证

~~~bash
go test ./...
go vet ./...
npm --prefix web ci
npm --prefix web run build
git diff --check
~~~

也可以执行：

~~~bash
make test
make build
~~~
