# Sidervia 测试与验收方案

- 状态：实现与发布门禁基线
- 版本：0.1
- 日期：2026-07-16

## 1. 测试目标

测试体系必须证明：

- 协议兼容和转换语义符合已声明能力，而不是只证明 HTTP 200。
- 账号调度、OAuth、资源绑定和价格计算在并发与重启后仍确定正确。
- 上游、下游和数据库失败不会泄漏 goroutine、连接、并发槽、临时文件或秘密。
- 目标 VPS 规格能够支撑不超过 5 个下游用户的实际流式工作负载。
- 每个发布镜像与源码测试结果一致。

## 2. 测试分层

### 2.1 单元测试

覆盖纯逻辑和边界条件：

- 配置解析、URL/IP 分类、Header policy。
- ID/Client Key 生成和解析、高熵 token verifier、AES-GCM envelope。
- OAuthAttempt 状态机、刷新窗口和 token rotation。
- CapabilitySet、Canonical IR 和协议错误映射。
- 调度硬过滤、优先级、负载率、轮转和冷却算法。
- Resource ID 类型、绑定查找和生命周期。
- Usage parser、Price Rule 匹配、定点金额与聚合。
- 日志/审计字段 redaction。

时间、随机数和网络不得在纯逻辑测试中读取全局状态；通过最小 `Clock`/`Random`/transport seam 注入确定值。

### 2.2 Repository 测试

每个测试使用独立临时 SQLite 数据库，执行真实迁移和 foreign key：

- 新库从零迁移。
- 每个已发布 Schema fixture 升级到当前版本。
- 事务失败和进程中断点模拟。
- 乐观并发冲突、busy timeout、Usage 批量写和聚合幂等。
- 主密钥错误、密文篡改和离线 key rotation。
- 365 天保留、资源到期和 OAuthAttempt 清理。

不使用与 SQLite 行为不同的内存 mock 替代 Repository 集成测试。可以使用 `:memory:` 做纯查询单测，但发布门禁必须覆盖真实文件、WAL 和重启。

### 2.3 Provider 契约测试

为每个 Provider 建立自有 fake upstream server，能够脚本化：

- 认证成功/失败和 token refresh/rotation。
- 非流式 JSON、chunked/SSE、WebSocket、媒体上传下载。
- 429/reset、5xx、慢 header、慢流、断流和异常 Content-Type。
- 模型发现、usage、资源创建/查询/删除。
- 未知字段和新事件。

fixture 根据官方公开文档由 Sidervia 项目独立编写。不得从 CLIProxyAPI、Sub2API 或其测试目录复制响应、Schema 或 golden file。

每个 Provider adapter 的 capability snapshot 必须对应一组契约测试；没有通过测试的能力不能声明为 supported。

### 2.4 协议兼容测试

测试使用官方 SDK 或最小标准客户端调用本地 Sidervia，再由 fake upstream 验证：

- 请求 Header、路径、模型、内容块和流式事件符合目标协议。
- Native response path 保留未知 Provider JSON 字段/事件且剥离敏感字段；官方 Provider 的未知 request field 返回 `unknown_request_field`，采样/布尔/停止序列等已允许控制项拒绝错误 JSON 类型，自定义兼容 Upstream 只转发管理员显式允许的字段路径。
- 下游 User-Agent、SDK telemetry、客户端版本字段和先前响应的 status/stop reason 不会进入上游请求；出站标识不得冒充官方 SDK/CLI。
- Converted path 的共有语义保持，已知降级产生 warning，不可表示能力返回 422。
- Error、finish/stop reason、tool call/result、structured output、reasoning 和 usage 映射正确。
- Client 取消和 backpressure 能传到 fake upstream。

官方 SDK 只用于黑盒兼容验证，核心适配器不因此强制依赖该 SDK。

### 2.5 端到端测试

启动真实 Sidervia 二进制、React 构建产物、SQLite 和 fake Provider：

1. 首次启动与管理员登录/TOTP。
2. CRUD Upstream/Proxy/Account/Model Route/Client Key。
3. OAuth 添加、刷新、失效和重新认证。
4. 公开 API 非流式、SSE、WebSocket 和文件流。
5. 多账号调度、并发、冷却和强资源绑定。
6. 用量、价格版本、Dashboard 和审计查询。
7. SIGTERM、SIGKILL、重启和恢复。

浏览器测试只覆盖关键用户流程，不为每个视觉细节创建脆弱的像素快照。

## 3. Provider 与能力矩阵

每个格子只能是：

- `native-tested`：原生路径通过官方协议契约测试。
- `converted-tested`：Canonical IR 双向转换通过语义测试。
- `planned`：设计存在，尚未实现。
- `unsupported`：明确不提供或官方不支持。
- `beta`：技术测试通过，但上游稳定性/授权边界未达到 Stable。

能力矩阵至少包含：

| 能力 | OpenAI | Anthropic | Gemini | xAI | OpenAI-compatible |
| --- | --- | --- | --- | --- | --- |
| 文本非流式/流式 | beta | beta | beta | beta | planned |
| 图片/文档输入 | planned | planned | planned | planned | planned |
| Tool Use | planned | planned | planned | planned | planned |
| Structured Output | planned | planned | planned | planned | planned |
| Reasoning controls | planned | planned | planned | planned | planned |
| Responses/state | planned | planned | planned | planned | planned |
| Files/Batches | planned | planned | planned | planned | planned |
| Embeddings | planned | planned | planned | planned | planned |
| Images/Audio/Video | planned | planned | planned | planned | planned |
| Realtime/Live | planned | planned | planned | planned | planned |
| API Key | beta | beta | beta | beta | planned |
| OAuth/Device/WIF | planned | planned | OAuth Code + PKCE beta；Device/WIF planned | planned | unsupported |

矩阵在实现开始后由自动测试生成或校验，README 不手工宣称超出矩阵的能力。

## 4. 调度测试

使用固定 Clock、固定候选顺序和可检查的 semaphore：

- 所有硬过滤原因逐一测试，并验证原因优先级。
- `billing_kind=subscription` 默认 priority 10、`metered/custom` 默认 20；认证方式变化不能改变计费类型，设置相同值后按负载率选择。
- `in_flight/max_concurrency` 的 0、满载和相同比率。
- round-robin 在并发下不越界，统计差异在允许范围。
- 软亲和命中、过期、候选失效和重启丢失。
- strong binding 覆盖亲和/优先级，绑定账号不可用时不换号。
- semaphore 获取与请求启动之间取消，必须释放所有状态。
- 429/5xx 冷却、官方 reset、抖动边界和成功清零。
- 并发刷新与调度读取账号快照，不产生 data race。

性质测试：Scheduler 返回的账号必须属于过滤结果；没有候选时必须返回稳定原因码，不能 panic 或返回零值账号。

## 5. OAuth 与认证测试

### 5.1 假 IdP

实现标准 Authorization Code + PKCE 和 Device Flow 假 IdP，支持：

- 正常授权、拒绝、过期、slow_down。
- state/PKCE 错误和重复 callback。
- 登录和手工 callback `POST` 必须同源；IdP 的公开 callback `GET` 不依赖 Origin/Referer，仍须通过 state、PKCE、Attempt 时效和所绑定管理 Session 有效性校验。
- access/refresh 到期、旋转、撤销和 identity 变化。
- token endpoint 慢响应、断连、5xx、格式错误。

### 5.2 并发场景

- 100 个请求同时发现 token 即将到期，只调用一次 refresh。
- refresh 期间管理员禁用账号，成功 token 不能把账号错误恢复为 active。
- refresh token rotation 后旧请求晚到，不得覆盖新凭证。
- Provider OAuth 配置及 Attempt 创建/取消的审计写入失败时，业务变更与审计事件同事务回滚。
- 401 refresh 重试与后台 refresh 竞争，最终 credential version 单调增加。

### 5.3 出口一致性

fake proxy 为每个连接记录身份，断言服务端 token exchange、refresh、validation、quota 和 inference 都使用 Account 解析出的同一 Egress Profile。浏览器 authorization 不纳入该断言；修改代理后，新快照使用新连接池，旧活跃请求安全结束。

## 6. 协议测试重点

### 6.1 Native path

- JSON unknown field 的对象、数组、null、大整数和 Unicode 保持语义。
- 模型和资源 ID 只修改允许位置，相同字符串出现在普通文本中不得被替换。
- 重复模型/认证关键键被拒绝。
- 下游认证、Cookie、Host、Forwarded、逐跳和私有 Header 不到达 fake upstream。

### 6.2 Canonical IR

为每个已声明映射建立双向 golden：

- system/instructions 与多轮 role。
- 文本、图片和文档引用。
- 并行 tool calls、tool result 关联和 tool choice。
- JSON Schema/structured output。
- reasoning effort/预算的可表示子集。
- stream delta、结束原因和 usage。

golden 断言语义字段，不依赖 JSON 键顺序或无意义的空值差异。

### 6.3 Stateful resources

- 创建成功后返回 Sidervia ID，不泄露上游 ID。
- 重启后 GET/DELETE/continue 命中同一账号。
- 其他 Client Key 使用同一 Sidervia ID 时得到不存在等价错误，不能确认资源归属。
- 无 model 的创建请求按显式路由提示/端点默认路由选择；两者缺失时返回 `route_required`。
- stream 中首次 Response ID 建立 binding 后才能向下游发送。
- 绑定写入失败执行 orphan 处理并返回稳定错误。
- parent/child binding 清理顺序正确。

## 7. 流式和故障测试

每个测试结束后采集 goroutine、连接、临时文件、Usage queue 和 account in-flight：

- 上游首包前网络失败。
- 首个下游字节前/后分别发生 401、429、500 和断连。
- 下游慢读、立即取消、读一半取消。
- 上游永不结束、定期心跳和长时间无数据。
- SSE event 跨任意 chunk 边界。
- WebSocket 双向同时发送/关闭、ping timeout 和大 frame。
- 进程 SIGTERM 有序关闭与 timeout 强制取消。
- SIGKILL 后重启，SQLite 和临时目录恢复。

用 `go.uber.org/goleak` 或等价机制只在确实需要时加入依赖；优先通过显式 WaitGroup、metric 和测试生命周期断言泄漏。

## 8. 用量与价格测试

- 每种 unit kind 的正常、缺失、负数、溢出和精度边界。
- Provider reported cost 优先且不与本地估算重复相加。
- 精确、fallback、冲突和无匹配 Price Rule。
- micro-USD 乘法、分段价格、batch discount、priority multiplier 和舍入。
- 同一请求写入幂等；Usage writer 重试不产生重复 line items。
- 日/月聚合与明细整数和一致。
- 发布 Price Catalog v2 后，v1 历史请求仍绑定 v1。
- 365 天清理的基础日聚合、明细删除和审计同事务；usage 畸形、聚合溢出或审计失败时回滚并报警，重复运行不重复累计。

## 9. 前端测试

- TypeScript strict、组件单测和 API contract 类型生成/校验。
- 登录、TOTP、Session 过期和 CSRF 失败。
- TOTP 同一 time-step 并发重放只有一次成功；本机 `--disable-totp` 恢复后全部 Session 失效。
- 页面刷新可通过 Session endpoint 恢复 CSRF，浏览器持久存储仍不含 token。
- Secret 一次显示、复制后不进入 URL/Local Storage。
- Account 状态、路由确认、价格发布和高风险设置的交互。
- 所有上游可控文本的 XSS payload。
- 关键页面在 1280px 桌面和窄屏可操作；不把像素完全一致作为门禁。

Playwright 端到端测试使用真实构建资源和 fake backend/upstream，不访问生产 Provider。

## 10. 性能与容量验收

### 10.1 基准环境

- 2 vCPU、2 GiB RAM、40 GiB SSD。
- 正式 release 二进制/镜像，关闭 debug profiler。
- Caddy 与 Sidervia 同机；fake upstream 在独立进程并注入固定延迟。
- SQLite 使用生产 PRAGMA，数据库预置 365 天量级的代表性元数据。

### 10.2 场景

1. 20 个 SSE 连接，持续 10 分钟，每秒事件和周期 usage。
2. 5 个 WebSocket/Realtime 双向连接，持续 10 分钟。
3. 10 RPS 非流式小请求，同时 Usage writer 和 Dashboard 查询。
4. 2 个大文件流式上传/下载，同时进行普通推理。
5. 账号 429/5xx 风暴、刷新 singleflight 和冷却写入。
6. 10,000/100,000 请求明细下的查询、聚合和 365 天清理。

### 10.3 通过标准

- 20 个并发流连续运行无 OOM、deadlock、race 或连接/槽位泄漏。
- 普通代理自身处理 p95 ≤ 50 ms；SSE 附加 TTFT p95 ≤ 50 ms。
- 稳态 RSS 目标 ≤ 512 MiB（不含操作系统页缓存和明确临时文件）。
- Usage queue 不持续满载，SQLite busy 不导致连续公开请求失败。
- 停止负载后 60 秒内 goroutine、连接和内存回到基线允许区间。

如果不通过，先分析 buffer、连接池、事务和查询，不以引入 Redis/PostgreSQL 作为默认修复。

## 11. CI 矩阵

每个 PR：

- Go 当前固定版本（1.26.x）、Linux amd64。
- Go unit/integration、race、vet、format check。
- React lint/typecheck/unit/build。
- SQLite migration from zero 和最近发布 fixture。
- 安全门禁见 [security-audit.md](security-audit.md)。

主分支/Release 增加：

- Linux arm64 构建与最小运行测试。
- 容器端到端、backup/restore、升级。
- Provider contract 全矩阵。
- 性能 smoke；完整容量测试至少在 release candidate 执行。

## 12. 发布验收记录

每个版本保存：

```text
version / commit / image digest
Go/Node/package manager/SQLite versions
test command and result links
capability matrix snapshot
migration source and target versions
security gate result
performance environment and summary
backup/restore result
known limitations and accepted risks
```

测试失败不得通过重跑掩盖。先定位是否 flaky；修复后补回归测试，确属环境问题才允许带证据重跑。

## 13. 当前 v0.2 证据边界

v0.2 已实现四家 Provider 的本地协议契约、假上游 JSON/SSE 集成、Client Key、调度、固定出口、Google OAuth 和基础 usage 测试；精确证据见[实现状态](implementation-status.md)。`beta` 不代表真实付费账号线上互通、2C2G 容量、长时间 soak 或独立安全复核已经完成。Canonical IR、资源、媒体、WebSocket 和成本相关章节仍为 planned，不得借由现有原生文本路径标记为已通过。

当前开发门禁：

```bash
make check
make test-race
```

CI 还必须验证冻结前端锁文件、OpenAPI 类型无漂移、`govulncheck`、生产依赖审计、Docker 构建和 non-root entrypoint。本机 arm64 开发镜像已经完成启动与恢复演练；Linux CI、正式多架构镜像、容量和完整安全扫描仍是发布阻断项。
