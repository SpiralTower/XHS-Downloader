# Go 核心 API、React 用户端与管理端

本仓库的核心 REST API 使用 Go，浏览器端使用 React 19、HeroUI v3、Tailwind CSS v4 和 Vite 8。Go 服务同时提供：

- 用户端：`/`
- 管理端登录：`/admin/login`
- 管理设置：`/admin/settings`
- 作品统计与版本：`/admin/works`
- 解析历史：`/admin/history`
- API 文档：`/docs` 与 `/openapi.json`

原 Python 桌面版、TUI、CLI 与 MCP 代码仍保留，不属于 Go 容器镜像。

## 工具链

- Go 1.24 或更高版本；`go.mod` 固定 Go 1.24 工具链
- Node.js 22 与 npm 10
- Python 3.12 或更高版本，仅用于保留的 Python 功能
- Docker 26 或兼容版本

CNB 云原生开发环境由 [`.ide/Dockerfile`](../.ide/Dockerfile) 提供 Go 1.24、Python 3.12、Node.js 22 与 Docker CLI。

## 本地开发

安装前端依赖、运行测试并构建：

~~~bash
npm --prefix web ci
npm --prefix web test
npm --prefix web run build
~~~

管理密码没有默认值。启动 Go 服务前必须设置密码：

~~~bash
export XHS_ADMIN_PASSWORD="请替换为强且唯一的密码"
go run ./cmd/api
~~~

访问：

- 用户端：<http://127.0.0.1:5556/>
- 管理端：<http://127.0.0.1:5556/admin/login>
- API 文档：<http://127.0.0.1:5556/docs>
- OpenAPI：<http://127.0.0.1:5556/openapi.json>
- 健康检查：<http://127.0.0.1:5556/healthz>

新数据库默认关闭公共访问和“已有记录时重新抓取”。管理员登录后可按部署需要显式开启；已有数据库的设置不会被迁移覆盖。

前端热更新：

~~~bash
npm --prefix web run dev
~~~

Vite 默认监听 5173 端口，并把 `/api`、`/xhs` 与 `/healthz` 代理到 <http://localhost:5556>。

## 用户解析 API

推荐新客户端使用：

~~~text
GET  /api/v1/access
POST /api/v1/extractions
Content-Type: application/json
~~~

已登录管理员的请求级连接覆盖示例：

~~~json
{
  "url": "https://www.xiaohongshu.com/explore/作品ID",
  "connection": {
    "cookie": "仅本次请求使用的 Cookie",
    "proxy": null
  }
}
~~~

匿名请求必须省略 `connection.cookie` 和 `connection.proxy`（包括显式 `null`）；它们会继承管理端默认连接。请求级覆盖只对已登录、同源的管理员开放。

管理员请求中的 `connection.cookie` 与 `connection.proxy` 语义相同：

- 省略字段：继承管理端配置的默认值。
- 非空字符串：仅覆盖本次请求。
- 显式 `null`：本次禁用对应默认值，并且不使用持久缓存。
- 空字符串：请求无效。

请求级 Cookie 和代理不会写入解析历史，也不会在响应中回显。响应只说明连接来源是 `none`、`default`、`override` 或 `disabled`；匿名请求尝试覆盖时返回 HTTP 403。

用户接口没有下载开关。每次解析是否保存文案、图片、视频，以及是否重新抓取，完全由管理端设置控制。只有 Cookie/代理来源均为默认值或未配置时才使用持久缓存；请求级覆盖或显式 `null`（disabled）都会绕过它，解析记录与作品内容版本仍会写入 SQLite。

持久缓存 scope 是服务端 HMAC 摘要，覆盖默认/无连接上下文以及解析后 URL 的规范化授权查询（包括 `xsec_token` 等）。不同 token 或连接上下文不会互相命中；缓存表和解析历史不保存这些 Cookie、代理或授权 token 的明文。

支持小红书与 RedNote 的 `explore`、`discovery/item`、`user/profile/.../作品ID` 链接，以及 `xhslink.com` / `xhslink.cn` 短链。

## 兼容接口 `/xhs/detail`

旧客户端可继续调用：

~~~json
{
  "url": "https://www.xiaohongshu.com/explore/作品ID",
  "download": true,
  "index": [1, "3"],
  "cookie": null,
  "proxy": null,
  "skip": false
}
~~~

响应继续使用 `{message, params, data}` 结构，并从 `params` 中清除 Cookie 与代理。

兼容参数说明：

- `download=false`：只解析，不保存本次文案、图片或视频，即使管理端保存开关已开启。
- `download=true`：保存策略为“管理端允许的类别”与“本次允许下载”的交集。例如管理端只开启图片时，本次也只保存图片。
- `index`：仅在 `download=true` 时筛选图文作品的静态图片及对应实况视频，序号从 1 开始，可用整数或整数字符串；普通视频作品忽略该参数。为空时选择全部。
- `skip=true`：存在兼容下载记录时跳过作品处理。
- 已登录同源管理员可用非空 `cookie`、`proxy` 覆盖本次请求；匿名请求携带非空值时返回 HTTP 403。省略、空字符串或 `null` 时继承管理端默认值。

`download` 只控制本次资源保存，不改变管理端的“已有版本是否重新抓取”策略。每次有效请求仍会形成解析记录。

## 保存与资源限制

管理端可独立控制：

- 文案：在 `Volume/Download/<作品ID>/v<版本号>/work.json` 保存版本数据。
- 图片：保存图文静态图片，以及视频作品的主封面。
- 视频：保存普通视频和实况照片的视频部分。

SQLite 记录资源类别、序号、状态、MIME、大小与 SHA-256。管理 API 不返回服务端文件路径；作品页通过管理员认证的资源内容接口读取已保存缩略图。内容相同时复用作品版本，但每次查询仍保留独立的 `parse_runs` 记录。

成功的真实抓取和缓存命中会原子更新作品累计次数及 UTC 每日汇总；失败和兼容接口直接跳过不计数。首页热门榜单默认关闭，开启后提供累计 Top10 与可切换的近 30/7 天 Top10。

`XHS_MAX_MEDIA_BYTES` 限制每一个保存的图片或视频资源，默认 `2147483648` 字节（2 GiB）。服务同时校验上游 `Content-Length` 和实际流式读取量；超过上限的资源不会作为成功文件发布。

资源保存失败不会抹掉已成功解析的作品数据。新版 API 在 `version.resources[].save_error` 返回稳定错误码；兼容接口在 `download=true` 时通过 `data.下载错误` 返回稳定错误码。原始网络和文件系统错误只写服务端日志，不进入 API 响应或 SQLite。

## 访问与代理安全

- 新数据库默认关闭公共访问；管理员必须显式开启后，匿名用户才能使用用户端和解析 API。
- 匿名解析按 TCP 来源限制为每分钟 12 次（IPv4 按地址、IPv6 按 /64），整个实例每分钟 120 次，最多并发 4 个；超出时返回 HTTP 429 和 `Retry-After`。已登录管理员使用独立容量。
- `POST /api/v1/extractions` 与 `POST /xhs/detail` 共享匿名额度；不要默认信任 `X-Forwarded-For`。
- 关闭公共访问后，静态页面仍可加载，但解析需要管理员会话；所有携带有效管理员会话的解析请求都必须同源。
- 匿名用户不能覆盖或禁用默认 Cookie/代理；请求级覆盖仅限已登录同源管理员。
- 代理仅支持 `http://` 与 `https://`。
- 默认拒绝解析到私网、回环或链路本地地址的代理。
- `XHS_ALLOW_PRIVATE_PROXY=true` 只适用于受信本地环境，不应在公开服务中开启。
- 请求体超过 `XHS_MAX_BODY_BYTES` 时返回 HTTP 413；其他参数校验错误通常返回 HTTP 422。

## 运行配置

| 环境变量 | 默认值 | 说明 |
|---|---:|---|
| `HOST` | `0.0.0.0` | 监听地址 |
| `PORT` | `5556` | HTTP 端口 |
| `WEB_DIST_DIR` | `web/dist` | Vite 构建产物目录 |
| `XHS_VOLUME_DIR` | `Volume` | 下载、临时文件与兼容记录目录 |
| `XHS_DATABASE_PATH` | `<Volume>/Data/xhs.sqlite3` | SQLite 路径；解析后必须位于 `XHS_VOLUME_DIR` 内，符号链接逃逸会被拒绝 |
| `XHS_SECRET_KEY_PATH` | `<数据库目录>/secrets.key` | 默认连接加密密钥路径；未设置时由服务创建和管理 |
| `XHS_ADMIN_USERNAME` | `admin` | 管理员用户名 |
| `XHS_ADMIN_PASSWORD` | 无 | 管理密码；生产启动必须设置 |
| `XHS_ADMIN_PASSWORD_FILE` | 无 | 从文件读取管理密码，优先于 `XHS_ADMIN_PASSWORD` |
| `XHS_ADMIN_SESSION_TTL` | `12h` | 管理会话有效期 |
| `XHS_SESSION_COOKIE_SECURE` | `false` | 是否只通过 HTTPS 发送管理会话 Cookie |
| `XHS_REQUEST_TIMEOUT` | `15s` | 短链解析与作品页面抓取的单阶段超时 |
| `XHS_DOWNLOAD_TIMEOUT` | `30m` | 单个媒体任务取得下载槽后的总超时 |
| `XHS_DOWNLOAD_IDLE_TIMEOUT` | `60s` | 媒体响应体连续无读取进度的超时 |
| `XHS_ALLOW_PRIVATE_PROXY` | `false` | 是否允许代理解析到非公网地址 |
| `XHS_MAX_BODY_BYTES` | `1048576` | API 请求体上限 |
| `XHS_MAX_MEDIA_BYTES` | `2147483648` | 每个图片或视频资源的保存上限 |
| `XHS_PUBLIC_RATE_LIMIT_PER_MINUTE` | `12` | 每个 TCP 来源每分钟允许的匿名解析数；IPv6 按 /64 归并 |
| `XHS_PUBLIC_GLOBAL_RATE_LIMIT_PER_MINUTE` | `120` | 整个实例每分钟允许的匿名解析总数 |
| `XHS_PUBLIC_MAX_CONCURRENCY` | `4` | 匿名解析的非排队式全局并发上限 |

生产环境通过 HTTPS 反向代理访问时必须设置：

~~~text
XHS_SESSION_COOKIE_SECURE=true
~~~

若仍使用纯 HTTP，浏览器不会发送 Secure Cookie，管理登录看起来会立即失效。

## SQLite 与加密密钥

SQLite 默认启用 WAL、foreign keys、busy timeout 与 immediate 写事务。`XHS_DATABASE_PATH` 的解析后真实路径必须位于 `XHS_VOLUME_DIR` 内；服务会拒绝任何解析到卷外的路径或符号链接逃逸，以确保数据库与实例锁受同一个持久卷保护。数据库和默认密钥位于：

~~~text
/app/Volume/Data/xhs.sqlite3
/app/Volume/Data/secrets.key
~~~

未设置 `XHS_SECRET_KEY_PATH` 时，服务会生成 32 字节密钥并把它作为受管文件维护。若使用 Kubernetes、Swarm 或其他 Secret 管理器，可把一个预置的、恰好 32 个原始字节的文件只读挂载到容器，再设置：

~~~text
XHS_SECRET_KEY_PATH=/run/secrets/xhs-settings-key
~~~

显式指定的密钥必须在启动前存在，不能使用十六进制或 Base64 文本替代 32 个原始字节，并且容器 UID/GID 10001 必须可读。服务只读取外置密钥，不创建也不修改其权限。丢失或替换密钥后，SQLite 中已加密的默认 Cookie 与代理将无法解密。

## Docker

推荐使用 Compose：

~~~bash
cp .env.example .env
# 编辑 .env，至少设置 XHS_ADMIN_PASSWORD
docker compose up --build -d
docker compose ps
~~~

用户端位于 <http://127.0.0.1:5556/>，管理端位于 <http://127.0.0.1:5556/admin/login>。

直接运行镜像：

~~~bash
docker build -t xhs-downloader:local .
docker run --rm --name xhs-downloader \
  -p 5556:5556 \
  -e XHS_ADMIN_PASSWORD="请替换为强且唯一的密码" \
  -v xhs-volume:/app/Volume \
  xhs-downloader:local
~~~

外置只读 Secret 示例：

~~~bash
docker run --rm --name xhs-downloader \
  -p 5556:5556 \
  -e XHS_ADMIN_PASSWORD="请替换为强且唯一的密码" \
  -e XHS_SECRET_KEY_PATH=/run/secrets/xhs-settings-key \
  --mount type=bind,src=/绝对路径/xhs-settings-key,dst=/run/secrets/xhs-settings-key,readonly \
  -v xhs-volume:/app/Volume \
  xhs-downloader:local
~~~

最终镜像以 UID/GID 10001 的非 root 用户运行，并内置健康检查。宿主机绑定挂载必须允许该 UID/GID 读写持久目录；外置 Secret 只需可读。

### 单实例、单卷约束

该部署按一个应用实例、一个持久卷设计。服务启动后会持有 `/app/Volume/.xhs-downloader.lock` 的非阻塞进程锁；同卷第二实例会以非零状态退出，并报告 `is already in use by another application instance`。锁文件会保留，是否被占用以操作系统锁为准，不要在实例运行时删除或替换它。

卷锁是误启动保护，不是多副本协调机制。不要使用 `docker compose up --scale api=2`，也不要让多个主机或网络文件系统上的副本同时写一个 `Volume`。Compose 显式声明 `replicas: 1`、`stop-first` 和 15 秒优雅停止窗口；其他编排平台也必须保持单副本，并确保旧实例完全停止后再启动替代实例。

Compose 使用固定名称 `xhs-volume`。重启和升级必须继续挂载同一个卷，否则历史、下载文件和受管密钥会分离。

### 升级旧卷

旧卷由 root 创建时，UID 10001 可能无法写入。停止服务后可修复属主：

~~~bash
docker compose down
docker run --rm --user 0 \
  -v xhs-volume:/app/Volume \
  alpine:3.22 chown -R 10001:10001 /app/Volume
docker compose up --build -d
~~~

旧版 Compose 的卷名可能带项目名前缀，例如 `myproject_xhs-volume`。确认实际名称后迁移：

~~~bash
docker compose down
docker volume create xhs-volume
docker run --rm --user 0 \
  -v myproject_xhs-volume:/from:ro \
  -v xhs-volume:/to \
  alpine:3.22 sh -c "cp -a /from/. /to/ && chown -R 10001:10001 /to"
docker compose up --build -d
~~~

### 备份

停止唯一实例后备份整个 `Volume`，不要只复制主数据库文件。至少保留：

- `Data/xhs.sqlite3`、`xhs.sqlite3-wal`、`xhs.sqlite3-shm`
- `Data/secrets.key`，或对应的外置 Secret
- `Download/`
- `Temp/`
- 兼容的 `downloaded.json`（如果存在）

## CNB 构建

[`.cnb.yml`](../.cnb.yml) 使用显式的 CNB 数据卷格式，避免 path-only 卷被错误解析：

~~~yaml
volumes:
  - xhs-go-mod:/go/pkg/mod:copy-on-write
  - xhs-go-build:/root/.cache/go-build:copy-on-write
  - xhs-npm:/root/.npm:copy-on-write
~~~

流水线执行：

1. `go test ./...` 与 `go vet ./...`。
2. `npm --prefix web ci`、前端测试和生产构建。
3. 使用生产 Dockerfile 构建镜像。
4. 使用临时命名卷启动镜像并等待健康检查。
5. 在首实例健康时启动同卷第二实例，断言它因卷锁非零退出且日志包含明确冲突信息。
6. 停止首实例后复用同一卷重启，确认锁已释放且 SQLite 与 `secrets.key` 持久化。
7. 仅在 `master` push 且全部检查通过后推送提交短 SHA 与 `latest` 标签。

CNB Docker 服务会为当前仓库制品库提供登录状态；镜像地址使用 `$CNB_DOCKER_REGISTRY/$CNB_REPO_SLUG_LOWERCASE`。

校验流水线：

~~~bash
node /root/.agents/skills/cnb-pipeline/validator/validate.js /workspace/.cnb.yml
~~~

## 验证

~~~bash
go test -count=1 ./...
go vet ./...
CGO_ENABLED=0 go build ./cmd/api
npm --prefix web test
npm --prefix web run build
git diff --check
~~~

也可以执行：

~~~bash
make test
make build
~~~
