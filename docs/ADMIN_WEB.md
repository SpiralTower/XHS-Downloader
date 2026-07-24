# 用户端、管理端与 SQLite 运维说明

Go 服务同时提供用户解析页面、管理端和版本化历史数据库：

- 用户端：`/`
- 管理端登录：`/admin/login`
- 设置：`/admin/settings`
- 作品：`/admin/works`
- 作品版本详情：`/admin/works/:workId`
- 查询历史：`/admin/history`

## 首次启动

管理端没有默认密码。启动服务前必须设置强密码：

~~~bash
export XHS_ADMIN_USERNAME=admin
export XHS_ADMIN_PASSWORD="请替换为强且唯一的密码"
go run ./cmd/api
~~~

也可以设置 `XHS_ADMIN_PASSWORD_FILE` 从文件读取密码；该变量优先于 `XHS_ADMIN_PASSWORD`。

Docker Compose：

~~~bash
cp .env.example .env
# 编辑 .env，至少设置 XHS_ADMIN_PASSWORD
docker compose up --build -d
~~~

生产环境通过 HTTPS 反向代理访问时必须设置：

~~~text
XHS_SESSION_COOKIE_SECURE=true
~~~

如果仍通过纯 HTTP 访问，浏览器不会发送 Secure Cookie，管理登录会看起来立即失效。

新数据库默认关闭公开解析和重复抓取。首次登录后由管理员按部署需要开启；升级已有数据库不会覆盖现有设置。

## 页面与权限

- `/`：用户端；匿名用户只提交作品链接，已登录管理员可使用高级连接选项。
- `/admin/login`：管理端登录。
- `/admin/settings`：默认连接、公共访问、首页热门榜单、保存策略与重新抓取策略。
- `/admin/works`：唯一作品、成功解析次数、已保存标题/缩略图和最近解析时间。
- `/admin/works/:workId`：同一作品的版本时间线及每个版本关联的资源。
- `/admin/history`：每次解析请求的记录；旧 `/admin/history/:workId` 会重定向到作品详情。

“允许公开解析”默认关闭。关闭后静态页面仍可加载，但匿名解析会被服务端拒绝；已登录管理员仍可从同源用户端解析。不要只依赖前端隐藏按钮，权限由服务端执行。

开启公开解析后，每个 TCP 来源默认每分钟 12 次（IPv4 按地址、IPv6 按 /64）、整个实例每分钟 120 次，最多并发 4 个；两个解析入口共享额度，超出返回 HTTP 429 和 `Retry-After`。管理员会话使用独立容量。可通过 `XHS_PUBLIC_RATE_LIMIT_PER_MINUTE`、`XHS_PUBLIC_GLOBAL_RATE_LIMIT_PER_MINUTE` 和 `XHS_PUBLIC_MAX_CONCURRENCY` 调整。

开启“首页显示热门解析榜单”后，榜单条目会在新标签页打开该作品解析所得的规范小红书链接。

## 默认连接与请求覆盖

默认 Cookie 和代理使用服务端的 AES-256-GCM 密钥加密后写入 SQLite。管理 API 只返回是否已配置和脱敏后的代理主机，不返回原值。

请求级覆盖只对已登录同源管理员开放。新版用户 API `POST /api/v1/extractions` 的管理员覆盖规则：

- 省略字段：继承后端默认值。
- 非空字符串：仅覆盖本次请求。
- 显式 `null`：本次禁用对应默认值。
- 空字符串：请求无效。

请求级 Cookie、代理不会写入解析历史，也不会出现在响应中。使用任一请求级覆盖或显式 `null` 时，本次不会读取或更新持久缓存映射；作品版本和查询记录仍会保存。匿名请求携带这些字段（包括 `null`）会返回 HTTP 403。

旧兼容接口 `/xhs/detail` 对空字符串或 `null` 保持旧语义：视为未覆盖并继承默认值；匿名请求不能提交非空 Cookie/代理。其响应会清除 `params.cookie` 与 `params.proxy`。

## 保存策略

管理端可独立控制：

- 保存文案：为作品版本生成 `work.json`。
- 保存图片：保存图文静态图片，以及视频作品的主封面。
- 保存视频：保存普通视频和实况照片的视频部分。

文件按 `Volume/Download/<作品ID>/v<版本号>/` 保存。SQLite 记录资源类型、顺序、保存状态、MIME、大小与 SHA-256；管理 API 不暴露服务端文件路径。

新版用户接口没有下载参数，始终按管理端策略决定是否保存。开启公开解析并同时开启任一保存类别，意味着匿名解析会按该策略写盘；请结合限流、媒体大小和磁盘监控评估容量。旧 `/xhs/detail` 额外保留请求级门控：

- `download=false`：本次文案、图片、视频均不保存。
- `download=true`：实际保存类别是“管理端开启的类别”与本次下载请求的交集。
- `index`：仅在 `download=true` 时筛选图文的静态图片和对应实况视频；普通视频忽略该参数。

关闭某类保存策略不会删除或隐藏此前已经成功保存的文件；作品页仍可显示这些已存标题和缩略图。

每个图片或视频资源受 `XHS_MAX_MEDIA_BYTES` 限制，默认 `2147483648` 字节（2 GiB）。服务会同时检查上游 `Content-Length` 和实际读取量，超限资源不会作为成功文件发布。保存失败仍返回作品数据：新版 API 的 `version.resources[].save_error` 和兼容接口的 `data.下载错误` 只包含稳定错误码，原始网络/文件错误仅写服务端日志。

## 已有记录是否重新抓取

- 开启：重新请求上游。内容变化时生成新版本；内容相同则复用原版本，但仍新增一次查询记录。
- 关闭：在默认或无连接上下文已有缓存时，直接返回对应的最新版本，不请求作品页面。
- 管理员请求级 Cookie、代理覆盖或显式 `null`：始终绕过持久缓存映射。
- 只有旧下载标记、尚无快照时：仍抓取一次以补齐作品版本。

持久缓存只用于默认值或未配置的连接来源。scope 以服务端 HMAC 覆盖默认/无连接上下文和解析后 URL 的规范化授权查询（包括 `xsec_token` 等），SQLite 只保存摘要，不明文保存 Cookie、代理或授权 token。不同 token/连接上下文不会互相命中；`work_versions` 仍按 `(work_id, content_hash)` 去重，不会因 scope 复制相同内容版本。

## SQLite 数据模型

数据库默认位于 `Volume/Data/xhs.sqlite3`。`XHS_DATABASE_PATH` 解析后的真实路径必须留在 `XHS_VOLUME_DIR` 内，符号链接逃逸同样会被拒绝；数据库不能外置到卷锁保护范围之外：

~~~text
works
  └── work_versions
        └── version_resources

work_cache_scopes ──> work_versions
parse_runs ──> works / work_versions
work_parse_daily ──> works
~~~

- `parse_runs`：每次查询尝试，包括成功、失败、缓存或跳过。
- `works`：稳定作品 ID、累计成功解析次数与最近解析时间。
- `work_versions`：按规范化内容 SHA-256 去重的版本快照。
- `work_cache_scopes`：默认/无连接与授权查询 HMAC scope 到最新版本的缓存映射。
- `version_resources`：版本关联的文案、图片和视频资源及保存状态。
- `work_parse_daily`：按 UTC 自然日聚合成功抓取和缓存命中次数，用于近 7/30 天榜单。

`parse_runs` 仍只保留最近 10,000 条，但累计与每日统计不会随历史清理而减少。升级时只能从尚存的成功记录回填，已被旧版本清理的次数无法恢复。

旧 `Volume/downloaded.json` 会在启动时幂等导入为兼容下载标记，原文件不会被删除。

SQLite 启用 foreign keys、WAL、busy timeout 和 immediate 写事务。

## 加密密钥

未设置 `XHS_SECRET_KEY_PATH` 时，默认密钥位于数据库同目录的 `Data/secrets.key`，由服务生成并维护权限。它用于解密管理端保存的默认 Cookie 和代理。

如需使用外部 Secret：

1. 预置恰好 32 个原始字节的密钥文件；不要填写 64 字符十六进制或 Base64 文本。
2. 以只读方式挂载到容器，例如 `/run/secrets/xhs-settings-key`。
3. 设置 `XHS_SECRET_KEY_PATH=/run/secrets/xhs-settings-key`。
4. 确保容器 UID/GID 10001 能读取该文件。

显式路径必须在启动前存在。服务只读外置密钥，不会创建它，也不会强制 `chmod`。丢失或替换密钥后，数据库中的默认 Cookie 与代理无法恢复。

## 单实例与持久卷

部署定位为单应用实例、单持久卷。数据库真实路径必须位于 `XHS_VOLUME_DIR` 内，不能通过绝对路径或符号链接放到卷外；外置只读 `XHS_SECRET_KEY_PATH` 不受此限制。服务会持有 `Volume/.xhs-downloader.lock` 的非阻塞进程锁；同卷第二实例会非零退出并报告卷已被其他应用实例占用。锁文件不会在退出时删除，是否占用由操作系统锁决定。

- 不要使用 `docker compose up --scale api=2`。
- 不要让多个 Pod、容器、主机或网络文件系统客户端同时写一个 `Volume`。
- 滚动更新必须使用单副本和 `stop-first`，等待旧实例完全退出后再启动新实例。
- 不要在实例运行时删除或替换锁文件。
- 每次重启和升级必须挂载同一个卷。

卷锁只用于阻止误并发，不把应用变成多副本服务。`docker-compose.yml` 仍显式声明 `replicas: 1`、`stop-first`、15 秒优雅停止窗口和固定命名卷 `xhs-volume`；其他编排平台需要等价约束。

## 持久化与备份

停止唯一实例后再备份整个 `Volume`，不要只复制主 `.sqlite3` 文件：

- `Data/xhs.sqlite3`、`xhs.sqlite3-wal`、`xhs.sqlite3-shm`
- `Data/secrets.key`，或对应的外置 Secret
- `Download/`
- `Temp/`
- `downloaded.json`（如果存在）

数据库、加密密钥和下载文件必须作为一组恢复。

## HTTP API

公开与用户端：

~~~text
GET  /api/v1/access
GET  /api/v1/popular-works
POST /api/v1/extractions
POST /xhs/detail
~~~

管理端：

~~~text
GET|POST|DELETE /api/admin/v1/auth/session
GET|PATCH        /api/admin/v1/settings
GET              /api/admin/v1/history
GET              /api/admin/v1/works
GET              /api/admin/v1/works/:id
GET|HEAD         /api/admin/v1/resources/:id/content
~~~

管理会话使用同源 `HttpOnly`、`SameSite=Strict` Cookie。设置修改、登录、登出，以及所有携带有效管理员会话的解析请求都会校验请求来源。
