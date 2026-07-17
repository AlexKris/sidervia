# Sidervia 详细技术设计

- 状态：实现基线
- 版本：0.1
- 日期：2026-07-16
- 对应需求：[requirements.md](requirements.md)

## 1. 设计原则

1. **安全默认**：缺少主密钥、可信公网 URL 或必要校验时拒绝启动/拒绝启用账号，而不是带风险继续运行。
2. **原生优先**：Provider 原生能力走原生路径；只有共有语义进入 Canonical IR。
3. **状态可追踪**：账号选择、资源归属、价格版本和认证状态都必须持久可解释。
4. **流式优先**：不缓存完整模型响应或媒体，取消、背压和上游错误及时传播。
5. **单机优先**：围绕单 Go 进程和 SQLite 设计，不伪装成支持共享存储的分布式系统。
6. **独立实现**：协议依据官方公开文档和自有测试构建，不移植参考项目源码、Schema、API 或 UI。

## 2. 仓库与模块布局

当前目录按已实现职责组织；后续 Provider/协议包只在对应里程碑开始时创建，不提前建立空抽象层。

```text
sidervia/
├── cmd/sidervia/              # 服务与离线维护命令入口
├── internal/
│   ├── app/                   # 依赖装配、启动、关闭
│   ├── config/                # 环境/文件配置解析与校验
│   ├── auth/                  # 管理密码、会话、CSRF 与 TOTP
│   ├── control/               # 控制面资源领域服务
│   ├── cryptox/               # 加密 envelope、主密钥与 key ID
│   ├── httpapi/               # Admin API、中间件与公开端占位错误
│   ├── maintenance/           # doctor、备份与主密钥轮换
│   ├── store/                 # SQLite、迁移和 repository
│   ├── metrics/               # 私有 Prometheus endpoint
│   └── safelog/               # 结构化日志与最终脱敏
├── web/                       # React + TypeScript + Vite
├── migrations/               # go:embed 的只前进 SQL 迁移
├── docs/
└── test/                      # 跨包集成、假上游和协议 fixtures
```

后续 Provider auth、transport、native codec、Canonical IR、routing、resource 和 usage 必须保持为独立聚焦包。Provider 适配器位于 `internal/provider/<provider>`；不得把整套 Control/Store 对象传入适配器。

### 2.1 实现依赖基线

- HTTP 使用 Go 标准库 `net/http` 和小型自有中间件，不采用 Gin/Fiber 等全栈 Web 框架。
- 数据访问使用 `database/sql`、[`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) 和手写参数化 SQL；不引入 ORM。该驱动无 CGO，首次解析依赖时必须把它与其要求的 `modernc.org/libc` 精确版本一起锁定，并在 Linux amd64/arm64 镜像上执行数据库测试。
- Migration runner 保持项目内最小实现，SQL 通过 `go:embed` 打包。
- 前端使用 pnpm（在 `packageManager` 中固定版本）、React Router、TanStack Query、Radix 无样式可访问性 primitives 和 CSS Modules/design tokens。
- 不引入 Redux、微前端、运行时主题插件或整套重型后台模板。
- Admin OpenAPI 文档是 Go/TypeScript 接口契约；CI 校验生成的 TypeScript 类型没有漂移，Go handler 仍保持显式手写。

所有第三方依赖在首次引入时重新核对当前维护状态、许可证和可达漏洞；本文不固定未实际解析过的依赖版本。

## 3. 进程入口与配置

### 3.1 子命令

```text
sidervia serve                 启动服务（默认）
sidervia doctor                只读检查配置、数据库、主密钥和网络
sidervia backup create         通过 SQLite Backup API 创建一致性备份
sidervia backup verify         在临时数据库上执行完整性与解密抽样验证
sidervia key rotate            停机状态下事务化重加密敏感字段
sidervia admin reset-password  本机维护命令，通过 password file 重置密码；可显式关闭 TOTP
sidervia version               输出版本、commit、构建时间和 Schema 上限
```

除 `serve` 外的写命令必须获取数据目录独占锁。`doctor` 不修改数据。

### 3.2 核心环境变量

| 名称 | 默认值 | 规则 |
| --- | --- | --- |
| `SIDERVIA_DATA_DIR` | `/var/lib/sidervia` | 数据库、进程锁和临时目录根路径 |
| `SIDERVIA_LISTEN_ADDR` | `127.0.0.1:8080` | 推荐只监听反向代理后端 |
| `SIDERVIA_PUBLIC_URL` | 无 | 生产必填，HTTPS，OAuth 回调和安全校验基准 |
| `SIDERVIA_MASTER_KEY_FILE` | 无 | 生产必填，内容为 base64 编码的 32 随机字节，文件权限 0400/0600，且由运行 UID 所有 |
| `SIDERVIA_BOOTSTRAP_PASSWORD_FILE` | 无 | 仅空数据库首次启动读取；密码创建后应删除文件 |
| `SIDERVIA_TRUSTED_PROXIES` | 空 | CIDR 列表；空表示不信任任何转发头 |
| `SIDERVIA_LOG_LEVEL` | `info` | `debug/info/warn/error`；任何级别都不记录正文/凭证 |
| `SIDERVIA_METRICS_ADDR` | 空 | 空表示关闭；建议独立监听 `127.0.0.1` |
| `SIDERVIA_SHUTDOWN_TIMEOUT` | `30s` | 停止接收后等待活跃请求的上限 |

生产不接受把主密钥直接写在普通命令行参数中，避免进入 shell history 和进程列表。开发模式可以使用显式 `--dev`，但只允许回环监听且 UI 持续显示警告。

### 3.3 启动顺序

1. 解析配置并校验路径、URL、CIDR、权限和主密钥。
2. 获取 `${DATA_DIR}/sidervia.lock` 独占锁。
3. 打开 SQLite，设置 PRAGMA，检查 `quick_check`，执行嵌入迁移。
4. 验证主密钥能解密数据库中的 sentinel；空库则创建 sentinel。
5. 首次启动时从 bootstrap password file 创建管理员并立即散列。
6. 加载 Provider registry、路由和账号不可变快照。
7. 启动用量 writer、清理和刷新任务。
8. 就绪检查通过后开始接受公开流量。

迁移或主密钥验证失败时 `/readyz` 不得短暂返回成功。

## 4. 领域模型与状态

### 4.1 核心对象

- **Provider**：内置协议和认证实现，如 `openai`、`anthropic`、`google`、`xai`。
- **Upstream**：Provider 的一个网络端点和默认 Egress Profile。
- **Account**：属于一个 Upstream 的一组凭证、独立计费类型、优先级和并发策略。
- **Model Route**：唯一公开模型 ID 及其候选账号/上游模型集合。
- **Client Key**：下游调用身份。
- **Resource Binding**：Sidervia 资源 ID 到具体上游账号资源的强绑定。
- **Price Catalog**：有生效区间且不可变的计价规则版本。

### 4.2 Account 状态

持久状态 `account.status`：

```text
draft -> validating -> active
                     -> invalid
active -> reauth_required
active -> disabled
reauth_required -> validating -> active
invalid -> validating -> active
disabled -> validating/active（管理员显式启用）
```

`cooldown`、`quota_limited` 和 `saturated` 是运行时有效状态，不覆盖持久状态：

- `cooldown_until > now`：临时故障冷却。
- `quota_reset_at > now`：官方额度/限流窗口。
- `in_flight >= max_concurrency`：当前没有并发槽。

只有持久状态为 `active` 且运行时硬条件满足的账号可调度。

### 4.3 标识符

- SQLite 内部关系使用 `INTEGER PRIMARY KEY`。
- 对外对象使用 128-bit CSPRNG 随机值的无歧义小写 base32 表示，不含时间和数据库序号。
- 普通对象格式：`sdr_<kind>_<random>`，例如 `sdr_acct_...`。
- 上游协议对资源 ID 有固定前缀要求时，由对应 Resource ID Codec 生成兼容前缀；客户端必须把 ID 视为不透明字符串。
- Client Key 格式：`sk-sdr_<8-char-prefix>_<32-byte-base64url-secret>`。

ID 生成发生在加密前，使其可用于 AES-GCM AAD。

## 5. 公开 API 设计

### 5.1 下游鉴权

| 协议 | 首选位置 | 兼容位置 |
| --- | --- | --- |
| OpenAI/xAI | `Authorization: Bearer <Client Key>` | 无 |
| Anthropic | `x-api-key: <Client Key>` | Bearer |
| Gemini | `x-goog-api-key: <Client Key>` | `?key=` 仅显式开启 |

`?key=` 默认关闭，因为 URL 可能在 Sidervia 前面的代理中被记录。即使开启，Sidervia 也必须在访问日志前清除该值。

### 5.2 路由家族

第一阶段保留以下稳定入口家族：

- `/v1/models`
- `/v1/responses` 及相关 Responses 资源操作
- `/v1/chat/completions`
- `/v1/embeddings`
- `/v1/messages`、`/v1/messages/count_tokens`、`/v1/messages/batches/*`
- `/v1/files/*`、`/v1/batches/*`
- `/v1/images/*`、`/v1/audio/*`、`/v1/videos/*`
- `/v1/realtime` 及 Provider 官方支持的 WebSocket 子路径
- `/v1beta/models/*`、`/v1beta/files/*`、`/v1beta/batches/*` 等 Gemini 原生路径

这是一份路由命名空间设计，不表示每条路由在首个里程碑同时实现。Provider capability registry 决定实际开放集合；未实现路由返回明确错误，不返回伪成功。

### 5.3 模型解析

`public_model_id` 在 Sidervia 实例内区分大小写且唯一。解析顺序：

1. 精确匹配 Model Route。
2. 验证该 Route 声明支持当前协议和能力。
3. 取 Route Candidates，禁止按模糊名称或前缀自动猜测。

不同 Provider 恰好使用相同模型名但语义不同，管理员必须创建显式别名；系统拒绝创建第二个同名 Route。

不含 `model` 的资源创建接口（例如部分 Files/Batch 操作）按以下顺序解析：

1. 可选 `X-Sidervia-Route` 精确指定一个公开 Model Route；该头在出站前删除。
2. 管理员为 `(protocol, endpoint_kind)` 配置的默认 Model Route。
3. 两者都不存在则返回 `route_required`，不得根据现有账号数量或最近请求猜测。

后续资源操作只使用 Resource Binding，不再次读取默认路由。

### 5.4 请求 ID 与响应头

- 接受合法 `X-Request-ID`（ASCII、1–128 字符）或生成新的随机 ID。
- 向下游返回 `X-Request-ID`，向上游发送独立 request correlation ID，不泄露内部账号 ID。
- 已知的无损/有损转换警告使用 `X-Sidervia-Warning`，值为稳定原因码列表。
- 不暴露所选账号、代理地址或凭证类型；管理员可通过 Request ID 查看脱敏诊断。

## 6. 管理 API

管理 API 使用 JSON、`/api/admin/v1` 前缀和同源 Session Cookie。分页统一为 `limit`（默认 50，最大 200）和 opaque `cursor`。

### 6.1 认证接口

```text
POST   /api/admin/v1/auth/login
POST   /api/admin/v1/auth/logout
GET    /api/admin/v1/auth/session
PUT    /api/admin/v1/auth/password
POST   /api/admin/v1/auth/totp/setup
POST   /api/admin/v1/auth/totp/confirm
DELETE /api/admin/v1/auth/totp
```

登录请求包含密码和可选 TOTP。成功后设置 HttpOnly Session Cookie，并由 `GET /auth/session` 返回一次当前会话绑定的 CSRF token。密码或 TOTP 变更会增加 `session_version` 并吊销其他会话。

### 6.2 控制面资源

```text
/api/admin/v1/upstreams
/api/admin/v1/proxies
/api/admin/v1/accounts
/api/admin/v1/model-routes
/api/admin/v1/client-keys
/api/admin/v1/price-catalogs
```

所有资源支持 `GET list/get`；按需要支持 `POST create`、`PATCH update` 和 `DELETE`。删除有依赖的对象默认返回 `409 resource_in_use`，不级联删除历史记录。

Client Key 的 `POST` 响应只返回一次完整 secret。Account API 永远不返回 credential blob。

### 6.3 OAuth 接口

```text
POST   /api/admin/v1/oauth-attempts
GET    /api/admin/v1/oauth-attempts/{id}
POST   /api/admin/v1/oauth-attempts/{id}/callback
DELETE /api/admin/v1/oauth-attempts/{id}
GET    /oauth/callback/{provider}
```

创建请求指定 Provider、Upstream、Account 草稿、Egress Profile 和首选 flow。响应只包含 Attempt ID、授权 URL/device 信息、过期时间和前端展示指令。

### 6.4 观测接口

```text
GET /api/admin/v1/dashboard
GET /api/admin/v1/requests
GET /api/admin/v1/requests/{request_id}
GET /api/admin/v1/usage
GET /api/admin/v1/costs
GET /api/admin/v1/audit-events
GET /api/admin/v1/system/health
```

请求详情只包含元数据、阶段耗时、标准化调度解释和计量 line items，不提供 prompt/response 回放。

### 6.5 管理错误 envelope

```json
{
  "error": {
    "code": "stable_machine_code",
    "message": "safe human-readable message",
    "request_id": "...",
    "details": {}
  }
}
```

`details` 只能包含经过字段级允许列表的数据。堆栈、SQL、URL 中的凭证和上游原始错误正文不得返回浏览器。Admin JSON 请求体默认上限 1 MiB，拒绝未知字段、重复对象键、多个 JSON 值和尾随非空内容。

所有可修改控制面对象在响应中包含 `version` 并返回 `ETag: "v<version>"`。`PATCH`/`DELETE` 必须提交对应 `If-Match`；缺失或不匹配返回 `409 version_conflict`。

## 7. Provider SPI

Provider 能力拆成小接口，避免一个巨型 Adapter：

```go
type Descriptor interface {
    ProviderID() string
    Version() string
    Capabilities() CapabilitySet
}

type AuthDriver interface {
    Methods() []AuthMethod
    Start(ctx context.Context, req StartAuthRequest) (AuthAttempt, error)
    Exchange(ctx context.Context, req ExchangeRequest) (Credential, error)
    Refresh(ctx context.Context, cred Credential) (Credential, error)
    Validate(ctx context.Context, cred Credential) (AccountIdentity, error)
}

type NativeTransport interface {
    DoHTTP(ctx context.Context, req NativeHTTPRequest) (*http.Response, error)
    DialWebSocket(ctx context.Context, req NativeWebSocketRequest) (WebSocket, error)
}

type CanonicalCodec interface {
    Decode(protocol Protocol, raw RawRequest) (CanonicalRequest, []Warning, error)
    Encode(provider string, req CanonicalRequest) (NativeRequest, []Warning, error)
    DecodeResponse(provider string, raw NativeResponse) (CanonicalResponse, error)
    EncodeResponse(protocol Protocol, resp CanonicalResponse) (RawResponse, error)
}

type QuotaProbe interface {
    Probe(ctx context.Context, cred Credential) (QuotaSnapshot, error)
}
```

约束：

- `AuthDriver` 和 Transport 接收已经构造好的账号专属 Egress Client，不能自行使用全局默认客户端。
- 凭证以受控值对象传递，禁止实现 `String()` 输出 secret。
- CapabilitySet 是带 Schema 版本的静态声明；运行时发现结果只能收窄，不能扩大未经实现的能力。
- `QuotaProbe` 可选。没有官方稳定额度接口时返回 `unsupported`，不得推测虚假余额。

## 8. 数据模型

### 8.1 通用规则

- 所有表使用 snake_case，内部主键 `id INTEGER PRIMARY KEY`。
- 对外对象有 `public_id TEXT NOT NULL UNIQUE`。
- 时间为 UTC Unix milliseconds 的 `INTEGER`，列名以 `_at_ms` 结尾。
- 布尔值使用 `INTEGER NOT NULL CHECK (... IN (0,1))`。
- JSON 列必须带 `schema_version`，写入前由 Go 结构校验；核心过滤/关联字段必须规范化为独立列。
- 普通 JSON 列使用字段允许列表，不得保存凭证、签名 URL、Header、prompt/response/tool 内容或媒体；确有必要的敏感值只能进入用途单一的加密列。
- 外键开启，控制面默认 `ON DELETE RESTRICT`，短期 Attempt 可 `CASCADE`。
- 所有可修改控制面表包含 `version INTEGER NOT NULL DEFAULT 1`；成功修改原子加一。

### 8.2 表定义

#### `schema_migrations`

`version`、`name`、`checksum`、`applied_at_ms`。二进制发现数据库含更高版本迁移时拒绝启动。

#### `crypto_sentinel`

单行 `id=1`，保存 `key_id`、`ciphertext`。只用于验证主密钥是否匹配，不保存主密钥派生材料之外的秘密。

#### `system_settings`

`key TEXT PRIMARY KEY`、`value_json`、`version`、`updated_at_ms`。只保存非敏感、实例级设置；secret 和凭证必须进入专用加密列。

#### `admin_user`

单行 `id=1`：`password_phc`、`totp_secret_enc`、`totp_pending_secret_enc`、`totp_pending_expires_at_ms`、`totp_enabled`、`totp_last_used_step`、`session_version`、`failed_login_count`、`locked_until_ms`、时间戳。

TOTP 为 6 位、30 秒 time-step，验证窗口为当前 step ±1。成功验证时在同一事务内要求 `accepted_step > totp_last_used_step` 并更新该列，从而拒绝同一或更旧验证码重放。待确认 secret 最长保留 10 分钟。

#### `admin_sessions`

`public_id`、`token_verifier`、`csrf_token_enc`、`session_version`、`created_at_ms`、`last_seen_at_ms`、`idle_expires_at_ms`、`absolute_expires_at_ms`、`ip_prefix_hmac`、`user_agent_hmac`、`revoked_at_ms`。

索引：`token_verifier UNIQUE`、`absolute_expires_at_ms`、`revoked_at_ms`。

#### `client_keys`

`public_id`、`name`、`prefix`、`secret_verifier`、`status`、`created_at_ms`、`expires_at_ms`、`last_used_at_ms`、`revoked_at_ms`。

索引：`prefix UNIQUE`、`secret_verifier UNIQUE`、`status`。不保存明文 secret。

#### `egress_proxies`

`public_id`、`name`、`scheme`（http/https/socks5）、`host`、`port`、`username_enc`、`password_enc`、`tls_server_name`、`allow_insecure_tls`、`enabled`、时间戳。

`allow_insecure_tls` 默认 false，启用时 UI 持续显示高风险警告并写审计事件。

#### `upstreams`

`public_id`、`provider_id`、`name`、`base_url`、`default_proxy_id`、`allow_private_network`、`enabled`、`config_json`、时间戳。

唯一约束：`(provider_id, name)`。`base_url` 经过规范化，不允许 userinfo、fragment 或动态模板。

#### `accounts`

`public_id`、`upstream_id`、`name`、`auth_kind`、`billing_kind`（subscription/metered/custom）、`credential_enc`、`credential_expires_at_ms`、`proxy_id`、`status`、`priority`、`max_concurrency`、`identity_json`、`capability_version`、`last_validated_at_ms`、时间戳。

默认值：`billing_kind=subscription` 的账号 `priority=10`、`max_concurrency=1`；`metered/custom` 账号 `priority=20`、`max_concurrency=4`。数值越小优先级越高，管理员可改为相同值。`auth_kind` 不参与默认计费类型推断。

#### `account_runtime`

每账号一行：`account_id UNIQUE`、`failure_streak`、`cooldown_until_ms`、`quota_reset_at_ms`、`last_success_at_ms`、`last_error_at_ms`、`last_error_code`、`quota_json`、`updated_at_ms`。

`in_flight` 只在内存中维护，重启归零。

#### `account_models`

`account_id`、`upstream_model_id`、`capabilities_json`、`source`（discovered/manual）、`verified_at_ms`、`enabled`。唯一约束 `(account_id, upstream_model_id)`。

#### `model_routes`

`public_id`、`public_model_id UNIQUE`、`description`、`enabled`、`required_confirmation_at_ms`、时间戳。

当路由含多个候选时，`required_confirmation_at_ms` 必须非空，表示管理员明确确认候选代表同一公开模型。

#### `route_candidates`

`model_route_id`、`account_id`、`upstream_model_id`、`enabled`、`protocols_json`、`capabilities_json`、`created_at_ms`。唯一约束 `(model_route_id, account_id, upstream_model_id)`。

优先级来自 Account，不在 Candidate 重复保存。

#### `protocol_route_defaults`

`protocol`、`endpoint_kind`、`model_route_id`、`updated_at_ms`。唯一约束 `(protocol, endpoint_kind)`。只用于没有 model 且尚未产生 Resource Binding 的创建请求。

#### `oauth_attempts`

`public_id`、`admin_session_id`、`provider_id`、`account_id`、`flow_kind`、`state_verifier`、`pkce_verifier_enc`、`device_code_enc`、`egress_fingerprint`、`status`、`provider_payload_enc`、`created_at_ms`、`expires_at_ms`、`consumed_at_ms`。

`state` 只保存 SHA-256 verifier。Attempt 消费采用条件更新 `status='pending' AND expires_at_ms>now`，保证一次性。

#### `resource_bindings`

`public_id`、`resource_type`、`protocol`、`owner_client_key_id`、`provider_id`、`upstream_id`、`account_id`、`upstream_resource_id_enc`、`parent_binding_id`、`status`、`upstream_expires_at_ms`、`local_expires_at_ms`、`metadata_json`、时间戳。

索引：`public_id UNIQUE`、`(account_id,status)`、`local_expires_at_ms`。上游资源 ID 可能具有 bearer-like 能力，因此按敏感字段加密且不得进入普通日志。`metadata_json` 只允许资源类型、状态和非敏感能力元数据，禁止保存签名 URL、带凭证 query、Header、正文片段或媒体内容。

#### `request_records`

`public_id`（即 Request ID）、`client_key_id`、`protocol`、`endpoint_kind`、`public_model_id`、`provider_id`、`upstream_id`、`account_id`、`status_code`、`error_code`、`streamed`、`started_at_ms`、`first_byte_at_ms`、`completed_at_ms`、`request_bytes`、`response_bytes`、`usage_json`、`price_catalog_id`、`estimated_cost_microusd`、`routing_json`。

不含 URL query 原文、Header、prompt、response、tool arguments 或媒体内容。索引覆盖时间、Client Key、模型、账号和错误码。

#### `usage_line_items`

`request_record_id`、`unit_kind`、`quantity_decimal`、`unit_scale`、`rate_microusd`、`amount_microusd`、`price_rule_id`、`source`。

金额统一使用 `INTEGER micro-USD`，数量使用十进制定点字符串/scale，禁止浮点累计。

#### `usage_aggregates`

`bucket_kind`（day/month）、`bucket_start_ms`、`dimension_kind`、`dimension_id`、请求数、错误数、各 token/媒体计量、`cost_microusd`。唯一约束为 bucket + dimension。

#### `price_catalogs`

`public_id`、`version`、`currency`（v1 仅 USD）、`effective_from_ms`、`effective_to_ms`、`source_url`、`source_retrieved_at_ms`、`content_sha256`、`reviewed_at_ms`、`status`。发布后不可修改，只能创建新版本和关闭生效区间。

#### `price_rules`

`price_catalog_id`、`provider_id`、`model_pattern`、`endpoint_kind`、`unit_kind`、`tier_json`、`rate_microusd`、`conditions_json`。匹配必须确定；同一目录内相同优先级规则重叠时拒绝发布。

#### `audit_events`

`public_id`、`event_type`、`actor_kind`、`actor_id`、`target_kind`、`target_id`、`request_id`、`outcome`、`metadata_json`、`created_at_ms`。

metadata 使用每种事件的允许字段结构，不接受任意请求 JSON。

## 9. 加密、密码与密钥

### 9.1 主密钥

- 主密钥为 32 个随机字节，通过只读文件挂载提供。
- 启动时读取后立即关闭文件；内存中不生成可打印字符串。
- `key_id` 为 `SHA-256(master_key)` 的前 8 字节 hex，仅用于选择和诊断，不降低密钥强度。
- 主密钥不写入 SQLite、日志、诊断包、镜像或备份归档。

### 9.2 AES-GCM envelope

敏感字段使用 AES-256-GCM，每次写入生成独立 12 字节随机 nonce：

```text
version(1) | key_id(8) | nonce(12) | ciphertext_and_tag(n)
```

AAD：`sidervia:v1:<table>:<public_id>:<column>`。单例表使用稳定逻辑 ID `1` 代替 public ID。移动密文到其他行/字段会导致认证失败。

至少加密 Account credential、TOTP secret、Session CSRF token、代理用户名/密码、OAuth PKCE/device/provider payload 和上游资源 ID。加密字段不建立明文索引；需要查找时使用对应的 Sidervia public ID 或非敏感外键。

密钥轮换要求停止服务、创建已验证备份，然后运行 `sidervia key rotate`。命令在单事务中逐行解密/重加密并更新 sentinel；任意失败回滚全部更改。

### 9.3 管理员密码

使用 Argon2id PHC 字符串：初始参数 `m=65536 KiB, t=3, p=min(4,CPU), salt=16 bytes, output=32 bytes`。首次启动运行基准；若单次超过 1 秒只记录告警，不自动降低参数。

密码最少 14 个 Unicode code points、最多 256，拒绝全空白。系统不设置复杂度字符规则，但 UI 建议使用密码管理器生成的长随机密码。

### 9.4 高熵 token verifier

Client Key、Session token、CSRF token 和 OAuth state 都由 CSPRNG 生成至少 32 字节随机值，不使用 Argon2，也不依赖可轮换的加密主密钥。存储：

```text
SHA-256(full_random_token)
```

高熵使离线穷举不可行，且主密钥轮换不会使现有 Client Key 失效。Client Key 查找先按 8 字符 prefix 缩小集合，再常量时间比较 verifier。认证日志只记录 `client_key.public_id`。

### 9.5 管理会话

- Session token 32 随机字节，只在 Path=`/api/admin/`、Secure、HttpOnly、SameSite=Strict Cookie 中发送。
- 数据库存储 SHA-256 verifier，不存明文 token。
- idle timeout 12 小时，absolute timeout 7 天；每 5 分钟节流更新 last_seen。
- CSRF 使用 session-bound 32 字节随机 token：数据库只存 AES-GCM 密文，`GET /auth/session` 解密后返回，前端只在内存持有并通过 `X-CSRF-Token` 提交。这样页面刷新可恢复 token，且不在 Cookie/Local Storage 留副本。
- 登录及管理 API 的手工 OAuth callback `POST` 验证 `Origin`/`Referer` 与 `SIDERVIA_PUBLIC_URL`；IdP 发起的公开 callback `GET` 不能依赖同源请求头，而是验证高熵 state verifier、S256 PKCE、精确 redirect URI、Attempt 时效以及发起时绑定的管理 Session 仍有效。生产 Cookie 必须 Secure。
- 密码/TOTP 变更增加 `session_version`、吊销其他 Session，并轮换当前 Session token 与 CSRF token；本机恢复命令则吊销全部 Session。

### 9.6 登录防护

按可信来源 IP 和全局两个维度限速。默认单 IP 5 次/分钟、20 次/小时，全局 30 次/分钟、200 次/小时；只有同时通过两个额度检查的尝试才会原子记入两者，已被单 IP 限制拒绝的请求不消耗全局额度。在线 Argon2 校验/换密最多同时执行 2 个，槽满立即返回 429 且不消耗时间窗口额度，避免 2 GiB 主机被并发内存成本拖垮。连续失败计数在数据库中原子递增，并触发最长 15 分钟指数锁定。错误响应不区分密码错误、TOTP 错误或管理员不存在。

单管理员遗失 TOTP 时不使用在线恢复码。停服后运行 `sidervia admin reset-password --password-file <path> --disable-totp`，命令必须取得数据目录独占锁、验证主密钥、写入审计事件并吊销全部 Session。

## 10. OAuth 与账号生命周期

### 10.1 计划认证矩阵

| Provider/channel | API Key | 标准服务端身份 | 订阅/官方 CLI 登录 |
| --- | --- | --- | --- |
| OpenAI API / Codex | 支持 | Provider 官方开放时支持 | Beta；仅使用公开 OAuth/device 流程 |
| Anthropic API / Claude | 支持 | Workload Identity Federation | Claude App/Code 登录为 Beta |
| Google Gemini | 支持 | 标准 Google OAuth | Gemini CLI 类登录仅在官方允许时 Beta |
| xAI API / Grok | 支持 | 企业 OIDC 支持范围内 | Grok Build browser/device 登录为 Beta |
| OpenAI-compatible | 支持自定义 Header | v1 不提供通用自定义 OAuth | 不支持 |

“Beta”表示技术兼容和服务条款稳定性都未达到长期承诺。管理员必须明确启用并确认风险；系统不通过导入官方 CLI 文件、复制 Cookie 或伪造客户端来补齐不可用流程。

### 10.2 flow 选择

优先顺序：

1. Provider 官方公开 Device Authorization Grant。
2. 标准 Authorization Code + PKCE 且 VPS 公网回调可用。
3. 固定 localhost callback 无法到达 VPS 时，管理员手工粘贴完整 callback URL。

不支持账号密码、Cookie、Session Key、浏览器自动化或官方 CLI credential import。

### 10.3 OAuthAttempt

创建 Attempt 时：

- 生成 32 字节 state 和 PKCE verifier，challenge 使用 S256。
- state 原值只发给 IdP，数据库保存 SHA-256 verifier。
- Attempt 绑定 admin session、Provider、Account 草稿和 egress fingerprint。浏览器访问 IdP 授权页使用管理员浏览器网络；egress fingerprint 约束的是服务端 token exchange、refresh、validation 和 inference。
- Authorization Code flow TTL 10 分钟；Device flow 使用 `min(provider_expires_in, 10min)`。
- 同一 Account 同时只允许一个 pending Attempt，新 Attempt 原子取消旧 Attempt。

回调按 `state -> Attempt -> admin session -> egress -> PKCE` 顺序验证，随后条件更新为 `exchanging`。交换成功、验证账号身份并加密凭证后，在同一事务中更新 Account 和将 Attempt 标为 `consumed`。

由于管理 Cookie 使用 SameSite=Strict，来自外部 IdP 的顶层 callback 不要求浏览器携带 Session Cookie；服务端通过 Attempt 中保存的 admin session ID 确认原会话仍有效。callback 只写流程结果，管理 UI 后续轮询仍必须携带原 Session Cookie。手工 callback POST 则同时要求当前管理会话和 CSRF。

手工粘贴只接收完整回调 URL，服务端解析允许字段。UI 不解析 code，更不接收 token JSON。

### 10.4 凭证规范化

加密 blob 内部 Schema v1：

```json
{
  "schema_version": 1,
  "access_token": "...",
  "refresh_token": "...",
  "token_type": "Bearer",
  "scopes": ["..."],
  "expires_at_ms": 0,
  "provider_fields": {}
}
```

只有 AuthDriver 能读写该结构。`provider_fields` 使用 Provider 自有强类型 Schema 编解码，禁止无界日志序列化。

### 10.5 刷新算法

- 后台在 `min(5min, token_lifetime*10%)` 的提前窗口加随机抖动刷新。
- 请求发现即将过期时进入 per-account singleflight；等待者共享结果。
- 获取 singleflight 后重新读取最新凭证版本，避免重复使用旧 refresh token。
- 成功响应若旋转 refresh token，凭证 blob 与版本号在单事务中原子替换。
- `invalid_grant`、明确 revoked 或身份变化进入 `reauth_required`。
- 暂时网络/5xx 保留旧凭证并短冷却；仍在有效期内可继续使用，过期后不可调度。

### 10.6 401 行为

只有满足以下全部条件才刷新并重试一次：

1. AuthDriver 声明凭证可刷新。
2. 请求体可重放或尚未开始读取不可重放流。
3. 下游响应尚未提交。
4. 本次请求尚未执行认证重试。

第二次 401 立即标记 `reauth_required`。除此之外不在单个请求内透明切换账号；后续新请求由调度器重新选择，避免重复计费或创建重复资源。

## 11. 出站网络与 SSRF

### 11.1 Egress Client

缓存键为 `proxy public_id + normalized TLS/network policy`。每个键有独立 `http.Transport`、DNS 校验器和 WebSocket dialer。

连接时对每个解析结果执行：

- 默认拒绝 loopback、RFC1918、link-local、multicast、unspecified、IPv6 ULA 和云元数据地址。
- `allow_private_network=true` 只放开管理员明确配置的 Upstream host，不放开请求内 URL。
- 每次新连接重新解析并检查全部 IP，防止 DNS rebinding。
- HTTP redirect 默认不跟随；Provider 明确需要时仅允许同 scheme/允许 host 且重新校验。
- HTTPS 验证系统 CA；禁用验证属于显式高风险配置。

对于请求体内的图片/文件 URL，Provider 若自己拉取则 Sidervia 不预取；Sidervia 需要预取时使用独立 Fetch Policy 和严格 URL allowlist，不复用 Upstream 私网权限。

### 11.2 Header policy

采用“已知敏感/逐跳拒绝 + Provider 显式添加”策略。下游 Authorization、Cookie、Proxy 头、Host、Forwarded、SDK telemetry、客户端版本标识和 Sidervia 私有头永不透传。Provider 所需版本/实验头由 Codec 校验后加入；出站 `User-Agent` 如需发送，只能使用诚实的 `Sidervia/<major.minor>` 标识，不伪装官方 SDK/CLI。

未知业务 Header 默认不转发，避免身份和网络信息泄漏；JSON 字段按 13.1 的方向性策略处理。

## 12. 调度器

### 12.1 输入

```text
public_model_id
protocol
required_capabilities
optional affinity key
optional strong resource binding
request replayability
```

### 12.2 硬过滤顺序

1. Model Route 存在且启用。
2. Candidate 与协议/能力匹配。
3. Account/Upstream 启用，持久状态为 active。
4. 凭证未过期或可在请求前刷新。
5. strong binding（若有）与 Candidate 完全一致。
6. 不在 quota reset 或 cooldown 窗口。
7. 有可用并发槽。

过滤结果为空时按最具体原因返回：绑定账号问题 > 重新认证 > 能力不支持 > 额度重置 > 全部饱和 > 暂时不可用。

### 12.3 选择顺序

1. 有 strong binding：只选择绑定账号，不执行后续选择。
2. 有软亲和且亲和账号仍在候选：选择亲和账号。
3. 只保留最小 `priority` 值的层。
4. 计算 `load_ratio = in_flight / max_concurrency`，选择最小值。
5. 比率相同按每个 Model Route 的原子 round-robin cursor 选择。
6. 非阻塞获取账号 semaphore；竞争失败则从同层余下候选继续一次选择。

不使用随机 top-K、EWMA 延迟或复杂综合评分。健康和 reset 只作为硬过滤/次要状态，不让高延迟估计覆盖管理员优先级。

### 12.4 软亲和

- 来源为明确 `X-Sidervia-Session` 或协议中稳定且非强状态的会话 ID；缓存键始终加入 `client_key_id`，不同 Client Key 不能共享亲和。
- key 使用 HMAC 后存入进程内 LRU，值为 Account ID，TTL 30 分钟。
- 不从 prompt、IP 或 User-Agent 推断会话。
- 重启后亲和丢失是可接受行为；强资源绑定不受影响。

### 12.5 冷却规则

| 事件 | 行为 |
| --- | --- |
| 401/明确凭证错误 | 按第 10.6 节刷新一次；失败进入 reauth_required |
| 429/官方 quota | 优先使用 `Retry-After`/reset，限制在 1 秒至 24 小时 |
| 429 无 reset | 30 秒指数退避，最大 15 分钟，加 full jitter |
| 网络/502/503/504 | 2 秒指数退避，最大 2 分钟，加 full jitter |
| 其他 5xx | 同网络错误，但记录 Provider 原因码 |
| 调用方 4xx | 不惩罚账号；仅记录请求错误 |
| 成功 | 清零临时 failure streak；保留官方 quota reset |

冷却重要字段同步写入 `account_runtime`，避免重启造成限流风暴。

## 13. 协议与 Canonical IR

### 13.1 Native path

Native Codec 使用 `json.RawMessage`/流式 token parser 做结构检查和定点修改。请求与响应采用不同的未知字段策略。

请求侧：

- 提取模型、stream、状态资源 ID 和用量相关字段。
- 替换公开模型为 upstream model。
- 重写已知 Sidervia resource ID。
- 官方 Provider 只转发 capability snapshot 已验证的字段，以及官方文档明确允许承载扩展的对象。
- OpenAI-compatible 自定义 Upstream 可以配置额外 JSON field-path allowlist；仅保存路径，不保存示例值。未显式允许的未知顶层字段返回 `unknown_request_field`，不得静默丢弃或盲目透传。
- 拒绝重复安全关键键、超深 JSON 和超过限制的单字段。

响应侧：

- Native path 在剥离已知敏感字段后保留 Provider 新增 JSON 字段和事件的语义和值，但不承诺保留原始空白、键顺序或字节表示。
- Adapter 只从已知字段提取路由、用量和资源 ID；未知字段不会反向进入后续请求。
- 上游 HTTP status、stop reason、错误状态和客户端 metadata 不得自动回声到后续请求。只有官方协议明确要求且下游本次请求显式提供的语义字段才能进入请求。

字段策略本身带 Provider、协议和 `verified_against` 版本。发现未知请求字段时只记录低基数原因码和经过校验的字段路径摘要，不记录字段值；更新策略必须同时增加官方来源和契约测试。

v0.2 的 capability snapshot 只包含原生纯文本与 SSE：OpenAI/xAI Chat Completions、Anthropic Messages 和 Gemini GenerateContent 仅接受纯文本字符串或纯文本内容块，以及各端点已列入契约测试的采样/安全参数。工具、媒体、Structured Output、Reasoning、客户端 metadata/user/status、未知嵌套字段和未验证协议版本均在出站前拒绝。Anthropic 仅接受 `2023-06-01`，不转发任意 Beta 标头。

公开 Request ID 始终由 Sidervia 生成；下游提供的 `X-Request-ID` 不作为数据库幂等键，避免重复值折叠多次请求记录。v0.2 JSON 请求正文上限为 8 MiB，非流式响应为 32 MiB，单个 SSE 事件为 8 MiB，并在读取过程中执行上限而不是先无界缓冲。

### 13.2 Canonical IR v1

```go
type CanonicalRequest struct {
    Version      string
    Model        string
    Instructions []Block
    Turns        []Turn
    Tools        []Tool
    ToolChoice   ToolChoice
    OutputSchema *JSONSchema
    Reasoning    ReasoningOptions
    Stream       bool
    Metadata     map[string]string
}

type Block struct {
    Kind       BlockKind // text, image_ref, document_ref, tool_call, tool_result
    Text       string
    ResourceID string
    MediaType  string
    JSON       json.RawMessage
}
```

IR 不包含音频输出、实时会话、图片/视频生成、Embedding、Batch 或 File 生命周期，因为这些能力不具有跨 Provider 等价语义。

### 13.3 转换严格度

- 请求包含目标 Provider 无法表示的关键语义：`422 capability_not_supported`。
- 可证明不影响核心语义的元数据无法表示：继续请求并添加 `X-Sidervia-Warning`，同时写入调度诊断。
- Structured Output、Tool Choice 和 Reasoning 只有适配器通过契约测试后才标记可转换。
- 系统指令位置、工具结果关联和流式终止原因必须显式映射，禁止仅拼接成文本。

### 13.4 协议版本演化

Provider descriptor 保存 `verified_against` 日期和测试 fixture 版本。遇到未知响应事件：

- Native path 在安全边界内原样转发并记录计数。
- Converted path 若无法解释事件则安全终止为 `upstream_protocol_changed`，不能编造响应。

## 14. 流式、WebSocket 与媒体

### 14.1 HTTP/SSE

- Go Transport 禁止为流式路由设置会截断长响应的总 Client Timeout；改用连接、TLS、首包和 idle timeout。
- 读取上游后先验证状态和必要头，再提交下游响应。
- 使用固定 32 KiB copy buffer；需要检查 SSE 时使用增量 parser，单事件默认最大 8 MiB。
- 每次成功写下游即刷新适当的 ResponseFlusher；同时尊重下游背压，不创建无界 channel。
- 下游断开取消上游 context，释放 semaphore 和临时文件。

### 14.2 WebSocket

- 专用 handler 重建 Upgrade，不透传下游 hop-by-hop headers。
- 双向 pump 各自有有界消息大小、写超时和 ping/pong idle 检测。
- 一侧关闭会取消另一侧；关闭码在安全可表示时透传。
- 需要重写事件时只处理文本 JSON frame；未知 binary frame 仅在 Native capability 明确允许时透传。

### 14.3 上传与下载

- 已知 Content-Length 超过单接口限制时在读取前拒绝。
- Chunked body 使用 counting reader 强制上限。
- 能直接流式的 multipart 使用 `io.Pipe`，不调用会把整个文件读入内存的 helper。
- 只有必须重放/seek 时写入 `${DATA_DIR}/tmp`，文件名为随机 ID、权限 0600，不使用客户端文件名作为路径。
- 默认单临时文件上限 2 GiB、临时目录总上限 8 GiB、磁盘剩余低于 5 GiB 时拒绝新落盘任务；部署者可向下调整，向上调整需审计磁盘预算。
- 正常完成立即删除；后台每小时清理超过 24 小时的残留。

## 15. Resource Binding

### 15.1 创建

资源创建成功后，必须把 binding 与当前 Client Key 一并提交到 SQLite，再向下游返回 Sidervia ID。若 binding 写入失败：

- 不返回无法追踪的上游 ID。
- 尽力调用上游删除/取消，但失败只记录脱敏 orphan 审计事件。
- 返回 `resource_binding_failed`。

### 15.2 查找与重写

Resource ID Codec 按协议识别已知资源位置，包括路径参数、JSON 字段和 SSE event。每次替换校验资源类型，防止把 File ID 用在 Batch 操作。

每次读取、继续、取消或删除都校验 `owner_client_key_id`。不匹配返回资源不存在等价的协议错误，避免确认其他 Client Key 的资源是否存在。管理员重新分配单个 binding 需要重新认证敏感操作并写入包含旧/新 owner 的审计事件。

`previous_response_id` 被视为强绑定。响应中新产生的 Response ID 在返回前创建 binding；流式响应在首次出现 ID 时同步创建，失败则终止流。

### 15.3 生命周期

- `upstream_expires_at_ms` 来自官方响应或适配器规则；未知为 null。
- `local_expires_at_ms` 不得早于已知上游有效期，并为历史诊断保留额外 7 天。
- DELETE 成功将 binding 标为 deleted；不立即物理删除。
- 清理任务只删除已过期且没有活跃子 binding 的映射。

## 16. 用量与成本

### 16.1 Usage Event

热路径事件是有大小上限的强类型对象。Provider usage 先规范化为：

```text
input_tokens
output_tokens
cache_read_tokens
cache_write_tokens
reasoning_tokens
tool_calls by kind
image units by size/quality
audio seconds/chars
video seconds
embedding units/tokens
file storage byte-hours
service tier / batch marker
```

缺失值保持 unknown，不以 0 冒充。上游返回的总量与可分解量同时保存，Cost Engine 明确选择权威字段，避免重复计费。

### 16.2 价格匹配

匹配键：Provider、实际 upstream model、endpoint kind、unit kind、service tier、上下文/质量等条件。规则优先级：

1. 精确模型 + 精确 endpoint + 条件。
2. 精确模型 + 通用 endpoint。
3. 管理员明确创建的 wildcard。

同优先级多个规则匹配视为目录错误，请求仍完成，但成本标为 `unpriced` 并产生告警。

### 16.3 计算

- 使用十进制定点数量和 micro-USD 整数。
- 每个 line item 独立按规则规定的舍入方式计算，汇总只做整数加法。
- 官方响应含明确 cost 字段时保存为 `provider_reported`，同时可保存本地估算差异用于审计，但展示不重复相加。
- `billing_kind=subscription` 不改变计算公式，UI 标签为 `API-equivalent value`；认证方式本身不参与这一判断。

### 16.4 写入与保留

Usage writer 每 100 条或 2 秒提交一次。队列容量 1,000；满时请求结束路径同步写入一条短事务并增加 backpressure metric，不丢记录。

每日 UTC 00:10 执行保留任务。v0.2 在同一短事务中把满 365 天的明细累加到永久基础日聚合、写入清理审计并删除原行；任一聚合、校验或审计失败都回滚并保留明细，月视图可由日聚合求和。v0.3 引入价格和 line items 后，必须先生成对应的版本化日/月成本聚合，才能删除那些成本明细。

## 17. 错误模型

内部稳定原因码至少包括：

```text
authentication_failed
reauth_required
account_disabled
account_cooldown
quota_limited
all_accounts_saturated
model_not_configured
route_required
unknown_request_field
capability_not_supported
bound_account_unavailable
resource_not_found
resource_not_owned
resource_type_mismatch
upstream_timeout
upstream_unavailable
upstream_protocol_changed
request_too_large
temporary_storage_exhausted
price_not_found
internal_error
```

公开响应由入口协议映射为该协议的标准错误形状和合理 HTTP status。内部原因码可以放入安全扩展字段/响应头，但不得用错误内容泄露 Provider credential、Account ID、代理地址或上游原始正文。

上游错误正文只允许 Adapter 提取允许字段（type、code、safe message、request ID、retry/reset），随后立即丢弃原始 buffer。

## 18. Web 管理界面

### 18.1 技术约束

- React + TypeScript + Vite，路由和数据请求使用成熟轻量库；不引入微前端或运行时插件。
- 构建产物由 Go `embed.FS` 提供，`/api` 和公开协议路径永不回退到 `index.html`。
- Session token 只在 Cookie；CSRF token 只保存在运行时内存，刷新页面后重新从 session endpoint 获取。
- 所有 secret input 默认不可回显，浏览器自动填充按字段显式控制。
- 管理端提供 `zh-CN` 与 `en`，首次默认 `zh-CN`；手工选择只把非敏感 locale 保存到 Local Storage。

### 18.2 页面

1. 登录与 TOTP。
2. Dashboard：健康、并发、请求、错误、成本和待处理事项。
3. Accounts：添加方式、身份、模型、到期、刷新、quota、冷却、重新认证。
4. Upstreams & Proxies：网络配置和只读连通性测试结果。
5. Model Routes：公开模型、候选、协议/能力和明确分组确认。
6. Client Keys：一次性密钥创建、禁用、吊销和用量。
7. Usage & Cost：请求元数据、聚合、line items 和价格来源。
8. Price Catalog：草稿、冲突校验、发布和历史版本。
9. Audit & Settings：高风险变更、版本、备份状态和安全告警。

“测试连接”必须走服务端受控 Provider 验证，不允许管理员页面提交任意 URL 让服务器抓取。

## 19. 可观测性

### 19.1 日志

结构字段：timestamp、level、component、event、request_id、provider、endpoint_kind、status、duration_ms、error_code。Account/Client Key 使用内部脱敏 ID；不记录模型输入输出。

全局 redactor 在 sink 前再次扫描常见 bearer/API key/cookie/query key 模式，作为字段允许列表之外的最后防线。

### 19.2 指标

核心指标：

- 请求数、状态、协议、Provider、模型族。
- 总耗时和 TTFT histogram。
- 当前流/账号并发、候选过滤原因。
- OAuth refresh 成败、reauth_required 数量。
- cooldown/quota reset、上游错误。
- Usage queue 深度/同步降级写入。
- SQLite busy、写事务时长、WAL 大小。
- 临时目录字节数和清理结果。

指标 label 不使用 Request ID、Account ID、Client Key、完整模型任意字符串或 URL，防止高基数和敏感泄漏。

### 19.3 健康检查

- `/healthz`：进程事件循环可响应，不访问上游。
- `/readyz`：迁移完成、主密钥验证、数据库可读写、后台 writer 活跃、未进入关闭阶段。
- Provider/Account 健康显示在管理 API，不让单个上游故障使整个 Sidervia unready。

## 20. SQLite 事务与迁移

- DSN 启用 foreign keys、WAL、busy timeout 5 秒、synchronous NORMAL；备份/关键密钥操作可临时提升 FULL。
- 只开放有限连接：1 个 writer、最多 4 个 reader；用量批次不持有网络调用期间的事务。
- 管理写操作使用 `BEGIN IMMEDIATE`，先校验版本字段实现乐观并发，冲突返回 409。
- 每个迁移文件有递增版本、不可变 checksum 和 up SQL；生产不自动 down migrate。
- 迁移在真实旧版本 fixture 上测试，并在同一事务中执行；SQLite 不支持事务化的特殊操作必须拆成明确维护版本。

## 21. 关闭、恢复与故障边界

SIGTERM：

1. `/readyz` 立即失败并停止接受新请求。
2. 等待普通请求和流，最多 `shutdown_timeout`。
3. 取消剩余上游请求并关闭 WebSocket。
4. 刷新 Usage queue、停止后台任务。
5. 执行有界 WAL checkpoint，关闭 DB 和释放进程锁。

进程被 SIGKILL 时依靠 SQLite 原子性恢复。启动清理临时文件并把 `validating/exchanging` 等中间状态恢复为可诊断失败状态，不假定中断操作成功。

## 22. 实现顺序

1. 配置、SQLite/迁移、加密 sentinel、管理密码和进程生命周期。
2. Admin API、React 登录/基础 CRUD、Client Key。
3. Provider SPI 与 API Key 的 OpenAI/Anthropic/Gemini/xAI 原生文本/流式路径。
4. Model Route、Scheduler、并发/冷却与请求元数据。
5. OAuthAttempt、刷新、账号代理和重新认证。
6. Canonical IR 和共有语义转换。
7. Resource Binding、Files/Batches/Responses 状态操作。
8. 原生媒体、Realtime/Live、用量成本和价格目录完整化。
9. 安全加固、性能、备份、SBOM 和发布门禁。

每个阶段必须先满足[测试方案](testing.md)中对应门禁，不能等到 v1.0 才补协议和安全测试。

## 23. 官方协议基线

实现时以官方文档为准，并把每个适配器的验证日期固定在 capability snapshot：

- OpenAI：[Responses API](https://developers.openai.com/api/reference/responses/overview)、[Realtime API](https://developers.openai.com/api/docs/guides/realtime)、[Models](https://developers.openai.com/api/docs/models)、[Codex authentication](https://learn.chatgpt.com/docs/auth)
- Anthropic：[API overview](https://platform.claude.com/docs/en/api/overview)、[Authentication](https://platform.claude.com/docs/en/manage-claude/authentication)、[Messages](https://platform.claude.com/docs/en/api/messages)、[Message Batches](https://platform.claude.com/docs/en/api/messages/batches)、[Claude Code setup](https://code.claude.com/docs/en/getting-started)
- Gemini：[OAuth](https://ai.google.dev/gemini-api/docs/oauth)、[Live API](https://ai.google.dev/gemini-api/docs/live-api/capabilities)、[Batch API](https://ai.google.dev/gemini-api/docs/batch-api)、[Embeddings](https://ai.google.dev/gemini-api/docs/embeddings)
- xAI：[Enterprise authentication](https://docs.x.ai/build/enterprise)、[Batch API](https://docs.x.ai/developers/advanced-api-usage/batch-api)、[REST API](https://docs.x.ai/developers/rest-api-reference/inference)

官方文档中的登录能力不自动等价于允许第三方凭证池。合规判断和技术能力分别记录。
