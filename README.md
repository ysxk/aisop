# aisop

[github.com/ysxk/aisop](https://github.com/ysxk/aisop) 是一个纯后端的 Go 1.26 + PostgreSQL 16+ 商品与媒体管理服务。提供单管理员认证、商品分类、商品草稿/上架/下架、图片/视频上传、有序媒体关联及公开读取；**不包含普通用户注册、购物车、订单、支付或前端**。

## 模块结构

```text
cmd/aisop/                 CLI：serve、migrate、bootstrap-admin
api/                      嵌入二进制的 OpenAPI 契约及契约测试
internal/
  app/                    模块装配、路由、中间件、健康检查、清理调度
  auth/                   认证 repository / service / handler、会话、登录限流
  category/               分类 repository / service / handler、公开读取与管理
  config/                 环境变量解析与校验
  database/               PostgreSQL 连接、事务迁移与 migrations/*.sql
  httpapi/                严格 JSON 解码、统一错误、分页、请求 ID
  media/                  媒体 repository / service / handler、检查、删除队列
  product/                商品 repository / service / handler、媒体关联
  storage/                本地磁盘存储与目录边界
  testutil/               随机独立 schema 的 PostgreSQL 测试工具
.github/workflows/ci.yml   Go 检查和 PostgreSQL 16 集成测试
Dockerfile                多阶段构建与非 root 运行镜像
compose.yaml              PostgreSQL、一次性迁移、应用和持久卷
```

## 本地启动

需要 Go 1.26、PostgreSQL 16+、Bash、curl，以及自行安装的 [jq](https://jqlang.org/)。curl 示例使用 `--fail-with-body`（curl 7.76+）。先准备一个专用数据库及有迁移建表权限的数据库角色；本地原生运行的数据库须能从 `DATABASE_URL` 指定地址访问。

```bash
git clone https://github.com/ysxk/aisop.git
cd aisop
cp .env.example .env
chmod 600 .env
# 编辑 .env：替换数据库密码和 DATABASE_URL，不要使用占位值。
```

原生二进制和 Makefile **不会自动加载 `.env`**。以下命令在 Bash 执行；`.env` 会作为 shell 脚本读取，只加载自己信任的文件。不要提交 `.env`，也不要启用 `set -x`、curl `-v` 或打印凭据的终端记录。

```bash
set -euo pipefail
set +x
set -a
. ./.env
set +a

make build
./bin/aisop migrate

# 初始化唯一管理员：密码交互输入，不会出现在命令历史或命令行参数中。
read -r -p '管理员用户名: ' ADMIN_USERNAME
read -r -s -p '管理员密码（12–72 字节）: ' ADMIN_PASSWORD
printf '\n'
export ADMIN_USERNAME ADMIN_PASSWORD
./bin/aisop bootstrap-admin
unset ADMIN_USERNAME ADMIN_PASSWORD

# 前台运行；Ctrl-C 优雅停止。省略 serve 也是启动服务。
./bin/aisop serve
```

用户名先去除首尾空白，长度须为 1–100 个字符；密码为 **12–72 字节**，不是字符数，且不去除空白。数据库强制仅有一个管理员，没有注册或管理员管理接口。`bootstrap-admin` 在管理员已存在时拒绝执行，**不是密码重置命令**。迁移可重复执行；`serve` 不会替你执行迁移。

启动后可检查：

```bash
curl --fail-with-body --silent --show-error http://127.0.0.1:8080/healthz
curl --fail-with-body --silent --show-error http://127.0.0.1:8080/readyz
curl --fail-with-body --silent --show-error http://127.0.0.1:8080/openapi.yaml
```

`/healthz` 检查进程存活，`/readyz` 检查数据库连接和媒体目录可写。完整 API 契约在仓库 [`api/openapi.yaml`](api/openapi.yaml)，运行时 URL 为 **`http://127.0.0.1:8080/openapi.yaml`**（部署后替换主机）。

## 配置

以下是原生程序的实际默认值；环境变量为空时，有默认值的项使用默认值。所有 CLI 子命令都会加载并校验配置。

| 环境变量 | 默认值 | 校验与含义 |
| --- | --- | --- |
| `DATABASE_URL` | 无，必填 | PostgreSQL 连接串；连接时由 pgx 解析并检查可达性 |
| `ADDR` | `:8080` | 必须能按 `host:port` 拆分；监听时还须为有效地址 |
| `MEDIA_DIR` | `./data/media` | 本地媒体目录；启动时创建/打开，运行用户须可写；没有额外的字符串格式校验 |
| `SESSION_TTL` | `24h` | Go duration，闭区间 `1m`–`720h`（30 天） |
| `IMAGE_MAX_BYTES` | `10485760`（10 MiB） | 十进制整数，闭区间 `1`–`10737418240`（10 GiB），单文件字节数 |
| `VIDEO_MAX_BYTES` | `209715200`（200 MiB） | 同上，单文件字节数 |
| `ALLOWED_ORIGINS` | 空 | 逗号分隔、每项去首尾空白；精确 `http://host[:port]` 或 `https://host[:port]`，不允许用户信息、路径（含末尾 `/`）、查询或片段；不是通配符 |
| `ADMIN_USERNAME` | 无 | 仅 `bootstrap-admin` 使用；去首尾空白后 1–100 字符 |
| `ADMIN_PASSWORD` | 无 | 仅 `bootstrap-admin` 使用；12–72 字节 |
| `TEST_DATABASE_URL` | 无 | 仅测试工具使用；未设置时跳过 PostgreSQL 集成测试 |

`.env.example` 还提供 Compose 专用的 `POSTGRES_PASSWORD`（必填，无默认值）、`APP_BIND`（默认 `127.0.0.1`）、`APP_PORT`（默认 `8080`）。Compose 内部强制 `ADDR=:8080`、`MEDIA_DIR=/data/media`，并用 `POSTGRES_PASSWORD` 构造自己的 `DATABASE_URL`，不使用 `.env` 中供原生程序使用的连接串。

原生 `DATABASE_URL` 的用户名/密码中若有 URL 保留字符，必须正确进行百分号编码。**Compose 不负责 URL 编码：其 `POSTGRES_PASSWORD` 必须使用 URL-safe 字符（建议仅 `A–Z a–z 0–9 _ -`）**；`.env.example` 两处密码占位符应替换为同一个强随机值。示例 `sslmode=disable` 仅用于本地或 Compose 私有网络；生产数据库应按部署条件配置 TLS，不要把连接串写入日志。

## 完整 curl 工作流

在另一个 Bash 终端、仓库根目录执行以下各段，保持同一个 shell 会话。服务已完成迁移和管理员初始化。准备一个实际 JPEG/PNG/GIF/WebP 图片供上传；以下登录、上传、商品和媒体响应均为**直接对象**，没有 `data` 包装。

### 1. 登录并读取当前管理员

登录 JSON 使用 jq 从环境读取密码，避免手写 JSON 转义、shell 历史泄漏和密码进入 curl 参数。令牌保存在 shell 变量中，**不要 echo 或记录令牌**。

```bash
set -euo pipefail
set +x
BASE=http://127.0.0.1:8080

login() {
  local response
  response=$(jq -n '{username:env.ADMIN_USERNAME,password:env.ADMIN_PASSWORD}' |
    curl --fail-with-body --silent --show-error \
      -H 'Content-Type: application/json' --data-binary @- \
      "$BASE/api/v1/admin/auth/login")
  TOKEN=$(jq -er '.token' <<< "$response")
  jq '{token_type,expires_at,admin}' <<< "$response"
}

# Header 经 Bash 进程替换传给 curl，不把令牌放入 curl 参数，也不占用请求体 stdin。
auth_curl() {
  curl --fail-with-body --silent --show-error \
    --header @<(printf 'Authorization: Bearer %s\n' "$TOKEN") "$@"
}

read -r -p '管理员用户名: ' ADMIN_USERNAME
read -r -s -p '管理员密码: ' ADMIN_PASSWORD
printf '\n'
export ADMIN_USERNAME ADMIN_PASSWORD
login
unset ADMIN_PASSWORD
auth_curl "$BASE/api/v1/admin/auth/me" | jq .
```

### 2. 创建草稿、上传单个文件、修改元数据

`price_minor` 为非负整数最小货币单位，CNY 的 `19900` 表示 **199.00 元**；不要传 `199.00` 或浮点金额。SKU 必须唯一，重复运行前先完成后面的删除步骤或换 SKU。

```bash
PRODUCT_ID=$(auth_curl -X POST "$BASE/api/v1/admin/products" \
  -H 'Content-Type: application/json' \
  --data-binary '{"sku":"DEMO-001","name":"示例商品","description":"商品说明","price_minor":19900,"currency":"CNY","stock":10,"status":"draft"}' |
  jq -er '.id')

read -r -p '要上传的图片路径: ' IMAGE_FILE
MEDIA_ID=$(auth_curl -X POST "$BASE/api/v1/admin/media/images" \
  -F "file=@${IMAGE_FILE}" | jq -er '.id')

# multipart 只传一个 file；名称和替代文本通过 PATCH 单独修改。
auth_curl -X PATCH "$BASE/api/v1/admin/media/$MEDIA_ID" \
  -H 'Content-Type: application/json' \
  --data-binary '{"display_name":"商品正面","alt_text":"示例商品的正面图片"}' | jq .

# 管理员可在商品尚未公开时读取文件。
auth_curl -I "$BASE/api/v1/admin/media/$MEDIA_ID/content"
```

视频上传使用 `POST /api/v1/admin/media/videos`，同样只传一个 `-F "file=@路径"`，支持 MP4/WebM；下面的封面示例必须使用图片 ID。

### 3. 替换关联、设置封面、上架与公开读取

```bash
jq -n --arg id "$MEDIA_ID" '{media_ids:[$id],cover_media_id:$id}' |
  auth_curl -X PUT "$BASE/api/v1/admin/products/$PRODUCT_ID/media" \
    -H 'Content-Type: application/json' --data-binary @- | jq .

auth_curl -X PATCH "$BASE/api/v1/admin/products/$PRODUCT_ID" \
  -H 'Content-Type: application/json' --data-binary '{"status":"published"}' | jq .

curl --fail-with-body --silent --show-error "$BASE/api/v1/products?page=1&limit=20&q=DEMO" | jq .
curl --fail-with-body --silent --show-error "$BASE/api/v1/products/$PRODUCT_ID" | jq .

# 文件读取不需要管理员令牌，前提是仍有关联的 published 商品。
# 输出 HTTP 响应头，数据写到临时目录；单范围请求通常返回 206。
DOWNLOAD_DIR=$(mktemp -d)
curl --fail-with-body --silent --show-error -D - \
  -H 'Range: bytes=0-1023' \
  "$BASE/api/v1/media/$MEDIA_ID/content" -o "$DOWNLOAD_DIR/first-1024.bin"
curl --fail-with-body --silent --show-error -I "$BASE/api/v1/media/$MEDIA_ID/content"
rm -rf "$DOWNLOAD_DIR"
unset DOWNLOAD_DIR
```

`media_ids` 数组顺序就是展示顺序，最多 100 个且不可重复；封面必须是数组中的图片。省略 `cover_media_id` 或传 `""` 表示无封面，不可传 `null`。整个替换失败时保留原有关联。

### 4. 下架、验证不可公开访问、解绑和删除

```bash
auth_curl -X PATCH "$BASE/api/v1/admin/products/$PRODUCT_ID" \
  -H 'Content-Type: application/json' --data-binary '{"status":"archived"}' | jq .

# 本示例媒体只关联这个商品，因此两次均应显示 404。
curl --silent --show-error -o /dev/null -w '商品 HTTP %{http_code}\n' \
  "$BASE/api/v1/products/$PRODUCT_ID"
curl --silent --show-error -o /dev/null -w '媒体 HTTP %{http_code}\n' \
  "$BASE/api/v1/media/$MEDIA_ID/content"

# archived 商品仍保留引用，必须先解绑才能删除媒体。
auth_curl -X PUT "$BASE/api/v1/admin/products/$PRODUCT_ID/media" \
  -H 'Content-Type: application/json' --data-binary '{"media_ids":[],"cover_media_id":""}' | jq .
auth_curl -X DELETE "$BASE/api/v1/admin/media/$MEDIA_ID" -o /dev/null -w '删除媒体 HTTP %{http_code}\n'
auth_curl -X DELETE "$BASE/api/v1/admin/products/$PRODUCT_ID" -o /dev/null -w '删除商品 HTTP %{http_code}\n'
```

删除成功返回 204。媒体被**任意状态**商品引用时删除返回 409；删除商品会解除关联，但不会自动删除媒体文件。

### 5. 修改密码（撤销全部会话）、重新登录与退出

```bash
read -r -s -p '当前密码: ' CURRENT_PASSWORD
printf '\n'
read -r -s -p '新密码（12–72 字节）: ' NEW_PASSWORD
printf '\n'
export CURRENT_PASSWORD NEW_PASSWORD
jq -n '{current_password:env.CURRENT_PASSWORD,new_password:env.NEW_PASSWORD}' |
  auth_curl -X PUT "$BASE/api/v1/admin/auth/password" \
    -H 'Content-Type: application/json' --data-binary @- \
    -o /dev/null -w '改密 HTTP %{http_code}\n'
unset CURRENT_PASSWORD TOKEN

# 改密成功为 204；包括当前令牌在内的全部会话都已失效。
ADMIN_PASSWORD=$NEW_PASSWORD
export ADMIN_PASSWORD
unset NEW_PASSWORD
login
unset ADMIN_PASSWORD

auth_curl -X POST "$BASE/api/v1/admin/auth/logout" \
  -o /dev/null -w '退出 HTTP %{http_code}\n'
unset TOKEN ADMIN_USERNAME PRODUCT_ID MEDIA_ID IMAGE_FILE
unset -f login auth_curl
```

退出成功为 204，撤销当前会话；密码修改撤销**所有**会话。令牌为 32 字节加密随机数的无填充 URL-safe Base64（非 JWT），只在登录响应中返回；数据库只保存其 SHA256 十六进制摘要。每次受保护请求查询数据库并检查有效期，无本地认证缓存；过期必须重新登录，退出或改密后也不能继续使用旧令牌。

## 商品分类

分类为**平级分类**，每个商品可属于一个分类，也可不分类。分类名称去除首尾空白后须唯一（大小写敏感，1–100 字符），公开描述最多 2000 字符，`sort_order` 为非负整数、默认 0。分类元数据均可公开读取，包括没有上架商品的分类；不要把内部备注写入分类描述。分类列表按 `sort_order ASC,name ASC,id ASC` 排列。

升级已有部署时，先备份并停止应用，使用新版二进制执行 `./bin/aisop migrate`，再启动服务。新增 `003_categories.sql` 迁移不会删除或修改已有商品内容，原有商品默认未分类。

商品创建和 PATCH 接受 `category_id`：

- UUID：分配或迁移到该分类；分类不存在返回 409。
- 空字符串 `""`：清除分类；创建时也表示未分类。
- 省略：创建时未分类，PATCH 时保持原分类。
- `null`：返回 400，与其他 JSON 字段的约定一致。

商品详情及列表始终返回 `category_id`，未分类时为 `""`。商品列表支持 `?category_id=<UUID>`，或 `?category_id=none` 查询未分类商品；可与分页、`q` 和管理员 `status` 筛选同时使用。公开商品接口无论如何筛选都只返回已上架商品。

下面示例使用前文登录步骤中的 `BASE`、`TOKEN` 和 `auth_curl`，请在退出前执行：

```bash
CATEGORY_ID=$(auth_curl -X POST "$BASE/api/v1/admin/categories" \
  -H 'Content-Type: application/json' \
  --data-binary '{"name":"鞋靴","description":"鞋靴商品","sort_order":10}' | jq -er '.id')

CLASSIFIED_ID=$(jq -n --arg category "$CATEGORY_ID" \
  '{sku:"CATEGORY-DEMO",name:"示例运动鞋",price_minor:29900,category_id:$category}' |
  auth_curl -X POST "$BASE/api/v1/admin/products" \
    -H 'Content-Type: application/json' --data-binary @- | jq -er '.id')

curl --fail-with-body --silent --show-error "$BASE/api/v1/categories?limit=20" | jq .
auth_curl "$BASE/api/v1/admin/products?category_id=$CATEGORY_ID" | jq .
# 新建商品默认草稿，所以此公开筛选不会包含刚创建的商品。
curl --fail-with-body --silent --show-error "$BASE/api/v1/products?category_id=$CATEGORY_ID" | jq .

auth_curl -X PATCH "$BASE/api/v1/admin/categories/$CATEGORY_ID" \
  -H 'Content-Type: application/json' --data-binary '{"name":"运动鞋","sort_order":0}' | jq .

# 被任意商品（含草稿、下架商品）引用的分类不能删除，返回 409；先迁移商品或清除分类。
auth_curl -X PATCH "$BASE/api/v1/admin/products/$CLASSIFIED_ID" \
  -H 'Content-Type: application/json' --data-binary '{"category_id":""}' | jq .
auth_curl -X DELETE "$BASE/api/v1/admin/categories/$CATEGORY_ID" -o /dev/null
auth_curl -X DELETE "$BASE/api/v1/admin/products/$CLASSIFIED_ID" -o /dev/null
unset CATEGORY_ID CLASSIFIED_ID
```

## API 路由速查

下表“管理员”均指 `Authorization: Bearer <token>`；只有登录接口不需要现有会话。

| 方法 | 路径 | 权限 / 用途 |
| --- | --- | --- |
| GET | `/healthz` | 公开，进程存活 |
| GET | `/readyz` | 公开，数据库及媒体目录就绪 |
| GET | `/openapi.yaml` | 公开，OpenAPI 契约 |
| POST | `/api/v1/admin/auth/login` | 公开，登录 |
| POST | `/api/v1/admin/auth/logout` | 管理员，撤销当前会话 |
| GET | `/api/v1/admin/auth/me` | 管理员，当前身份 |
| PUT | `/api/v1/admin/auth/password` | 管理员，改密并撤销全部会话 |
| GET | `/api/v1/categories` | 公开，分类分页列表 |
| GET | `/api/v1/categories/{id}` | 公开，分类详情 |
| GET / POST | `/api/v1/admin/categories` | 管理员，分类列表 / 创建 |
| GET / PATCH / DELETE | `/api/v1/admin/categories/{id}` | 管理员，分类详情 / 修改 / 删除 |
| GET | `/api/v1/products` | 公开，仅 published；`page`、`limit`、`q`、`category_id` |
| GET | `/api/v1/products/{id}` | 公开，仅 published |
| GET / POST | `/api/v1/admin/products` | 管理员，列表 / 创建；列表可加 `category_id`、`status` |
| GET / PATCH / DELETE | `/api/v1/admin/products/{id}` | 管理员，详情 / 更新状态及字段 / 删除 |
| PUT | `/api/v1/admin/products/{id}/media` | 管理员，原子替换顺序与封面 |
| POST | `/api/v1/admin/media/images` | 管理员，图片上传 |
| POST | `/api/v1/admin/media/videos` | 管理员，视频上传 |
| GET | `/api/v1/admin/media` | 管理员，媒体列表；`page`、`limit`、`kind` |
| GET / PATCH / DELETE | `/api/v1/admin/media/{id}` | 管理员，元数据 / 修改 / 删除 |
| GET / HEAD | `/api/v1/admin/media/{id}/content` | 管理员，任意媒体内容 |
| GET / HEAD | `/api/v1/media/{id}/content` | 公开，仅有关联 published 商品的媒体 |

JSON 请求需 `Content-Type: application/json`，上限 1 MiB；拒绝未知字段和显式 `null`。PATCH 省略字段表示不改，允许的零值（如 `price_minor:0`、`stock:0`、`description:""`）正常更新，至少提供一个字段。分页响应为 `{items,page,limit,total}`，默认 `page=1`、`limit=20`，上限分别 1,000,000 和 100；商品和媒体按 `created_at DESC,id DESC` 排序，分类按 `sort_order ASC,name ASC,id ASC` 排序。商品 `q` 按 SKU/名称字面子串搜索（最多 200 字符），`%` 和 `_` 不作为通配符。

错误结构为 `{"error":{"code":"...","message":"...","request_id":"..."}}`，可能带 `fields`；响应头带 `X-Request-ID`。常见状态：400 参数、401 凭据/会话、404 不存在或不可公开读取、409 冲突/仍有引用、413 太大、415 类型不支持、416 范围无效、429 登录限流、503 未就绪。详细字段、返回状态及 Range/ETag 条件请求见 OpenAPI。

## 媒体和安全边界

- 图片允许 JPEG、PNG、GIF、WebP，默认最大 **10 MiB**。检查真实内容类型、先检查尺寸，再解码**首帧**；最多 **25,000,000 像素**。这不是动画所有帧的完整检查；不支持 SVG/HTML。
- 视频允许 MP4、WebM，默认最大 **200 MiB**。流式保存并检查容器签名；**不执行全片解码、完整编解码器验证、杀毒扫描或转码**，通过上传检查不等于可安全播放或无恶意内容。
- multipart 只接受一个名称为 `file` 的文件字段；不要额外提交元数据字段。磁盘键由服务端生成，不使用客户端文件名作为存储路径。
- 媒体目录**不作为静态目录公开**。公开内容接口在每次请求（包括 HEAD 和条件请求）检查是否仍有关联的 published 商品；只关联草稿/下架商品、未关联或不存在时均不可公开读取。管理员可读取未公开媒体。
- 文件支持 Range、ETag、条件请求。不要让反向代理/CDN 覆盖文件的 `Cache-Control: private, no-cache, max-age=0, must-revalidate`，否则下架后的旧缓存可能绕过重新授权；已经被客户端下载的副本无法撤回。
- 登录按 **RemoteAddr 对端 IP 每分钟 10 次、全局每分钟 100 次**限流，计入失败/无效请求；返回 429 和 `Retry-After` 秒数。状态有界、仅保存在**每个进程内存**，重启会清空，多实例不共享；**不信任 X-Forwarded-For / X-Real-IP**。反向代理后所有客户端可能共享代理 IP 的额度，请在代理层另外配置合理限流，不要误以为改 XFF 就能识别真实用户。
- 生产使用 HTTPS 反向代理，保护数据库网络和媒体目录，限制上传请求体并合理配置超时。CORS 精确允许列表只控制浏览器跨域，不替代身份认证；不要记录 Authorization、登录/改密请求体或连接串。环境变量仍可能被同权限进程或容器管理员读取。

## Docker Compose 部署

需要 Docker Engine 和支持 `service_completed_successfully` 的 Docker Compose v2。先复制并编辑 `.env`，设置强随机、URL-safe 的 `POSTGRES_PASSWORD`（见配置说明）；Compose 自动读取 `.env` 作变量插值，但不会把整份文件直接传入应用。

```bash
# 在仓库根目录；此处不需要 source .env。
docker compose build
# 自动顺序：db 健康 -> migrate 成功退出 -> app 启动。
docker compose up -d app

# 一次性传入 bootstrap 凭据，不把它们写入常驻 app 服务环境。
set +x
read -r -p '管理员用户名: ' ADMIN_USERNAME
read -r -s -p '管理员密码（12–72 字节）: ' ADMIN_PASSWORD
printf '\n'
export ADMIN_USERNAME ADMIN_PASSWORD
docker compose run --rm -e ADMIN_USERNAME -e ADMIN_PASSWORD app bootstrap-admin
unset ADMIN_USERNAME ADMIN_PASSWORD

docker compose ps
curl --fail-with-body --silent --show-error http://127.0.0.1:8080/readyz
# 停止但保留数据库和媒体卷；不要对需保留的数据执行 down -v。
docker compose down
```

Dockerfile 以 Go 1.26 多阶段构建，运行镜像为 Debian bookworm-slim，使用 **UID/GID 10001:10001**，`/data/media` 已设置可写所有权。ENTRYPOINT 是 `aisop`，支持默认 `serve`、`migrate`、`bootstrap-admin`；迁移和 OpenAPI 已嵌入二进制，无需运行时复制源码。

Compose 使用 PostgreSQL 16，数据库 `aisop`、角色 `aisop`；数据库与媒体分别落在命名卷 `postgres_data`、`media_data`。数据库默认**不映射宿主端口**，应用默认只绑定 `127.0.0.1:8080`（可用 `APP_BIND`、`APP_PORT` 调整）。不要为了原生启动示例误以为 Compose 的数据库能直接通过宿主 5432 访问。生产通过 HTTPS 代理访问；宿主代理可连接本地端口，容器代理应使用同一受控网络的应用地址。

首次创建命名媒体卷会继承镜像目录所有权；已有卷或改用 bind mount 时，需由运维确保 UID 10001 可读写，不要用 `chmod 777` 规避权限。修改 `.env` 中的 PostgreSQL 密码不会自动修改已经初始化的数据库卷内角色密码，应按数据库流程轮换并同步连接配置。

当前存储适合**单实例 + 持久本地磁盘**。多实例若连接同一数据库，必须看到同一份可靠且权限一致的媒体存储；独立容器磁盘不能代替共享存储。共享卷的文件系统行为、故障恢复和代理限流需另行验证，本项目不提供分布式存储或分布式限流。

## 删除、崩溃恢复与备份

媒体删除会在同一 PostgreSQL 事务里删除媒体记录并写入 `file_deletions` 删除意图；成功后 API 不再提供该媒体。服务会尝试立即删除文件，并在**启动时及每分钟**重试数据库队列；磁盘删除暂时失败时可能晚于 204 完成，需监控清理警告和存储容量。

**本地文件系统与 PostgreSQL 不是分布式原子事务**。上传文件写入到数据库记录提交之间若进程崩溃，可能遗留未入库的孤儿文件，删除队列不保证能发现所有这类文件；不要直接把扫描到的未知文件在线批量删除，维护时应先暂停写入、核对媒体记录和删除队列后再处理。

备份时暂停所有实例的写入和后台清理（最简单是停止应用，保留数据库运行），将**数据库与整个媒体目录/卷作为同一一致性集合一起快照或备份**，然后恢复服务；不要只备份其中一方。恢复时一起还原到对应时间点，检查目录权限、迁移版本、就绪检查及文件可读性，再开放写入。`docker compose down -v` 会删除命名卷，不是备份命令。

## 开发与验证

```bash
make build          # bin/aisop
make fmt            # 格式化 Go 文件
make fmt-check      # 仅检查格式
make vet
make test           # go test -race ./...；未设置 TEST_DATABASE_URL 时跳过数据库集成测试
make run            # go run ./cmd/aisop serve，需自行加载环境
make migrate
make bootstrap-admin
```

集成测试只使用 `TEST_DATABASE_URL`，不会回退到应用的 `DATABASE_URL`。请使用专用测试数据库；角色须有在该数据库中 **CREATE SCHEMA** 的权限。每个使用数据库工具的测试创建随机名称的独立 schema、设置 search_path、执行迁移，并在清理时仅删除自己的 schema；不对业务表执行破坏性 TRUNCATE。异常中断可能留下测试 schema，确认没有测试运行后再按名字清理。

```bash
# 在安全编辑的本地环境中设置指向专用测试库的 TEST_DATABASE_URL，勿把真实密码写进历史。
# 例如把变量写入权限为 600 的受信任测试环境文件，再 set -a / source / set +a。
make integration-test  # 要求 TEST_DATABASE_URL 非空；go test -race -count=1 ./...
```

CI 配置使用 PostgreSQL 16 service，按 `go.mod` 安装 Go，依次检查 gofmt、执行 `go vet ./...`、`go test -race ./...`（包含数据库集成测试）、`go build ./cmd/aisop`。这些是工作流定义，不代表任一次执行结果；Docker 镜像和 Compose 需在有 Docker 的环境中实际构建及联调验证。
