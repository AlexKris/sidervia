# Sidervia 安全审计方案

- 状态：发布门禁基线
- 版本：0.1
- 日期：2026-07-16
- 适用范围：Go 服务、React 管理端、SQLite 数据、容器、CI/CD 和部署文档

## 1. 审计目标

审计必须回答以下问题，并为每项结论提供可复现证据：

1. 未授权调用方能否取得管理权限、使用其他 Client Key 或读取资源元数据？
2. 数据库、备份、日志、指标、崩溃输出或 UI 是否会泄露上游凭证和请求内容？
3. 自定义 Upstream、代理、媒体 URL 或 OAuth 回调能否被利用执行 SSRF、绕过 TLS 或访问云元数据？
4. OAuth state/PKCE、token rotation、并发 refresh 和重新认证是否可被重放、串号或降级？
5. Native pass-through、协议转换、SSE/WebSocket 和 Header 处理是否会产生请求走私、注入、内存放大或语义错配？
6. 强状态资源是否可能跨账号、跨 Provider 或跨资源类型访问？
7. 调度、冷却和并发限制是否在竞争、取消和异常路径上泄漏槽位或错误换号？
8. 构建与发布产物是否能追溯到已审查源码，依赖是否存在可达漏洞？

审计不是上线前一次性活动。设计审查、自动门禁、版本渗透测试和运行期复核共同构成完整方案。

## 2. 安全基线

- Web 控制使用 [OWASP ASVS 5.0.0](https://owasp.org/www-project-application-security-verification-standard/) Level 2 作为通用基线；凭证、加密和管理认证相关条目按 Level 3 的严谨度验证。
- API 风险覆盖 [OWASP API Security Top 10 2023](https://owasp.org/API-Security/editions/2023/en/0x11-t10/)，尤其是资源消耗、SSRF、配置错误和不安全消费第三方 API。
- Go 依赖可达漏洞使用官方 [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)；不能只依赖版本匹配型扫描。
- Provider 行为以官方公开协议为基线；订阅 OAuth 另做服务条款和授权范围审查，技术可用不等于合规可用。

审计记录引用 ASVS 时必须写完整版本化编号，例如 `v5.0.0-x.y.z`，避免标准升级后含义漂移。

## 3. 资产与信任边界

### 3.1 最高敏感资产

- `SIDERVIA_MASTER_KEY` 及其文件。
- OAuth access/refresh token、API Key、WIF 配置和代理密码。
- 管理员密码哈希、TOTP secret、Session token 和 CSRF token。
- Client Key 明文（仅创建时存在）。
- 请求正文、响应正文、工具参数和媒体流，即使系统承诺不持久化。

### 3.2 重要完整性资产

- Model Route 和 Account 优先级。
- Resource Binding 与 upstream resource ID。
- Price Catalog、Usage line items 和审计事件。
- Provider capability 声明和协议转换规则。
- CI workflow、发布密钥、镜像和 SBOM。

### 3.3 信任边界

| 边界 | 不可信输入 | 主要威胁 |
| --- | --- | --- |
| 下游 Client → Public API | Header、URL、JSON、multipart、SSE/WebSocket 行为 | 鉴权绕过、DoS、走私、注入、资源串号 |
| 浏览器 → Admin API | Cookie、CSRF、JSON、Origin | 会话劫持、CSRF、XSS、越权 |
| Sidervia → Upstream | DNS、TLS、代理、第三方响应 | SSRF、DNS rebinding、恶意/异常响应、凭证泄漏 |
| OAuth IdP → callback | code、state、error、redirect | 登录 CSRF、重放、串会话、code 注入 |
| 进程 → SQLite/Backup | 密文、Schema、迁移 | 明文泄漏、回滚、篡改、错误密钥 |
| Source → CI → Image | 依赖、workflow、构建器、发布 token | 供应链污染、产物不可追溯 |

## 4. 威胁模型

| ID | 威胁 | 预期控制 | 必须验证的结果 |
| --- | --- | --- | --- |
| T-01 | 猜测/窃取 Client Key | 高熵、SHA-256 verifier、限流、一次显示 | DB 泄漏不能还原；错误不存在时序区分 |
| T-02 | 管理登录暴力破解 | Argon2id、双维度限流、锁定、TOTP | 重启和并发请求不能绕过有效限制 |
| T-03 | CSRF/跨站管理操作 | SameSite Strict、Origin、session-bound token | 无 token/错 Origin/旧 token 全部失败 |
| T-04 | 持久/反射 XSS | React 默认转义、CSP、禁止不受控 HTML | 上游模型名/错误/审计字段不能执行脚本 |
| T-05 | OAuth 登录 CSRF/重放 | state verifier、PKCE、Session/出口绑定、一次消费 | callback、code、Attempt 均不可跨会话复用 |
| T-06 | Refresh token 竞争丢失 | singleflight、DB 重读、原子 rotation | 并发 100 请求只执行一次有效 refresh |
| T-07 | SQLite/备份泄漏 | AES-GCM + 外部主密钥、无正文 | 离线检查看不到 token/TOTP/代理密码 |
| T-08 | 密文搬移/篡改 | 行/列绑定 AAD | 复制或修改密文必然认证失败且不回显明文 |
| T-09 | 自定义 Upstream SSRF | URL 校验、每次 DNS/IP 检查、redirect 限制 | 私网/元数据/IPv6/重绑定绕过均失败 |
| T-10 | 代理滥用 | 管理员专用配置、加密认证、账号固定出口 | 下游不能按请求指定代理或覆盖目标地址 |
| T-11 | Header/request smuggling | 逐跳头重建、长度校验、标准服务器栈 | 冲突 CL/TE、CRLF、Upgrade 异常被拒绝 |
| T-12 | 上游恶意响应 | 大小/深度/事件限制、timeout、严格解析 | gzip bomb、超大错误、无限 SSE 不耗尽内存 |
| T-13 | Resource ID 串号 | 随机 ID、Client Key ownership、类型校验、强 Account binding | 不能跨 Client 或直接输入上游 ID 绕过绑定 |
| T-14 | 流启动后错误换号 | commit 状态门禁 | 任意已写字节路径都不发生 retry/failover |
| T-15 | 临时文件攻击 | 随机文件、0600、无跟随、配额、清理 | 路径穿越、symlink、磁盘填满均受控 |
| T-16 | 日志/指标泄密 | 字段允许列表、全局 redactor、低基数 label | 注入 canary secret 后全量扫描为零命中 |
| T-17 | 依赖/构建污染 | 锁文件、最小权限 CI、SBOM、签名/证明 | 发布产物可关联 commit，token 不暴露给 PR |
| T-18 | 上游政策规避 | 公开流程、无指纹伪装、Beta 合规门禁 | 实现不存在 Cookie/SessionKey/私有 API 后门 |

## 5. 审计阶段

### 5.1 阶段 A：设计审查

触发：首次实现、认证/加密/代理/资源绑定重大修改。

输出：

- 更新的数据流与信任边界图。
- 对每个新入口的资产、威胁、控制和失败模式。
- 需求 → 设计 → 测试追踪表。
- 未解决问题、责任人和阻断级别。

门禁：T-01 至 T-18 每项必须有明确实现位置和测试计划；“由框架处理”不是证据。

### 5.2 阶段 B：实现级安全审查

重点人工审查以下代码路径：

1. 密钥加载、KDF、AES-GCM AAD、密钥轮换和错误处理。
2. Admin Session、Client Key、TOTP、CSRF、Origin 和 Cookie。
3. OAuthAttempt、callback、device polling、refresh rotation 和 reauth 状态机。
4. URL/host/DNS/IP/redirect/proxy 校验和自定义 Dialer。
5. Header 重建、请求大小、压缩、SSE/WebSocket parser。
6. Resource ID 查找/重写及强绑定查询。
7. semaphore、singleflight、冷却持久化、context cancellation。
8. 日志、指标、审计和错误 envelope 的字段来源。
9. SQLite migration、动态 SQL、JSON Schema 与事务边界。
10. CI workflow 权限、第三方 action pinning 和发布凭证。

每个安全关键函数至少需要两人审查或一次独立二次审查；作者不能独自关闭自己的 High/Critical 发现。

### 5.3 阶段 C：自动化门禁

每个 PR：

```bash
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

前端执行类型检查、测试、锁文件一致性和生产依赖审计。CI 另执行：

- Secret scan：Gitleaks 或等价工具，包含历史增量。
- SAST：gosec 作为补充，结果需要人工确认，不替代审查。
- 多生态漏洞：OSV-Scanner 扫描 Go 与前端锁文件。
- 容器：Trivy/Grype 扫描最终镜像，而不是只扫构建阶段。
- IaC/Workflow：actionlint；GitHub Actions 权限和表达式使用 zizmor 或等价检查。
- SBOM：Syft 或构建系统生成 SPDX/CycloneDX。

Release：

- 对实际发布二进制再次运行 `govulncheck -mode=binary`。
- 验证 SBOM、checksum、镜像 digest 与源 commit 一致。
- 验证镜像非 root、无编译器/包管理器、无 `.git`、测试 fixture、密钥或 source map 泄密。

### 5.4 阶段 D：白盒与黑盒专项测试

使用隔离测试账号、假 Provider 和无生产数据的 staging 环境。测试脚本不得指向真实云元数据或未授权第三方目标。

执行第 6 节所有用例，并保留请求、预期、脱敏输出和版本信息。

### 5.5 阶段 E：部署审查

检查 Compose/Caddy、文件权限、主密钥挂载、端口、TLS、可信代理、备份、恢复、日志轮转和监控暴露。

使用与生产相同的镜像 digest 完成一次空机部署和一次带数据升级。部署者本地覆盖项必须在 `doctor` 输出中以安全结果显示。

## 6. 专项测试清单

### 6.1 管理认证与浏览器

- 密码边界：空、13/14 字符、超长、Unicode、NUL、规范化差异。
- Argon2 PHC 参数降级、损坏 hash、并发登录下 CPU/内存消耗。
- TOTP 时间窗口、重放同一 code、关闭/重新启用和恢复流程。
- TOTP 成功 step 必须事务化单调增加；同一或更旧 step 在并发登录中只能成功一次。
- Session fixation、登录后 token 轮换、过期、idle/absolute timeout、密码修改后吊销。
- Cookie 的 Secure/HttpOnly/SameSite/Path；生产 HTTP 启动应失败。
- CSRF：缺失/错误/其他 Session token、跨 Origin、simple form、JSON text/plain。
- 刷新页面后 `/auth/session` 可恢复解密后的 CSRF token；数据库和备份中只能出现其密文。
- CSP、frame-ancestors、MIME sniffing、缓存控制和敏感页面 back-forward cache。
- 把 HTML/JS payload 放入 Provider 名、模型名、错误 message 和 audit metadata，验证无 XSS。

### 6.2 Client Key 与公开 API

- prefix 冲突、大小写、截断、额外空白、多个 Authorization Header。
- 不存在、吊销、到期和刚吊销 key 的响应与时序。
- Anthropic/Gemini 兼容 Header 不能绕过统一 verifier。
- query key 默认关闭；开启后反向代理和 Sidervia 日志都不得出现值。
- 管理 API 使用 Client Key 必须失败；Session Cookie 调公开 API 不自动获得权限。

### 6.3 OAuth 与凭证

- state 缺失、错误、重复、过期、来自另一管理 Session。
- PKCE verifier/challenge 错误和 downgrade 到 plain。
- callback URL 含重复参数、fragment、userinfo、CRLF、超长 code。
- 两个 Attempt 并发、旧 Attempt 完成、device code slow_down/expired/denied。
- token endpoint 超时、5xx、非 JSON、超大 JSON、缺少 refresh token、refresh token rotation。
- 100 个请求同时遇到过期 token，只允许一次 refresh；等待者收到同一新版本。
- 401 在响应提交前只重试一次；提交一字节后绝不重试。
- 登录、刷新、quota、推理的出口 IP/代理 identity 一致。
- UI、Admin API、日志、trace 和数据库查询中不出现 code/token。

### 6.4 加密与数据库

- 使用错误主密钥、缺少 key file、权限过宽、空 key、非 32 字节 key。
- 数据库二进制扫描常见 token 前缀和测试 canary；结果为零。
- Resource Binding 的上游 ID 使用 bearer-like canary，确认只出现于密文；`metadata_json` 拒绝签名 URL、凭证 query、Header 和正文片段。
- 篡改 nonce/tag/ciphertext/AAD，必须失败且错误不含明文。
- 把 Account A 密文复制到 Account B 或其他列，必须失败。
- 密钥轮换中断、磁盘满、事务回滚、旧/新 key 分别验证。
- 备份不含主密钥；没有主密钥无法解密；有正确 key 可完整恢复。
- SQL injection、JSON path、LIKE wildcard、cursor 和排序字段全部使用允许列表/参数化。

### 6.5 SSRF、DNS 与代理

至少覆盖：

- `127.0.0.1`、`0.0.0.0`、RFC1918、169.254/16、IPv6 `::1`/ULA/link-local、IPv4-mapped IPv6。
- 十进制/八进制/十六进制 IP、混合编码、尾点域名、IDN、userinfo、空 host、超长 host。
- DNS 首次公网后切换私网、多个 A/AAAA 中混入私网、CNAME 链。
- 30x 到私网/HTTP/其他 host、循环 redirect、无 Location。
- HTTP Proxy CONNECT、SOCKS5 DNS 模式和代理认证失败。
- `allow_private_network` 只能放开指定 Upstream，不能让下游媒体 URL/redirect 继承权限。
- 云元数据域名/IP 和常见内部管理端口全部拒绝。

测试使用本地隔离网络模拟，禁止对真实元数据服务发探测。

### 6.6 HTTP、JSON、SSE 和 WebSocket

- 冲突 Content-Length/Transfer-Encoding、重复 Host、Header CRLF、非法 method/path。
- 超大 Header、深层 JSON、重复关键键、数字溢出、无效 UTF-8、gzip/deflate 解压炸弹。
- 上游返回超大错误正文、错误 Content-Type、提前 EOF、慢 header/slow body。
- SSE：超长 event、跨 chunk UTF-8、无终止换行、未知 event、用量事件重复、连接永不关闭。
- WebSocket：超大 frame、fragment flood、压缩炸弹、无 pong、双向同时 close、binary/text 类型混淆。
- 下游取消后 goroutine、连接、semaphore 和临时文件最终归零。
- Native response 的未知 Provider JSON 字段/事件能安全保留；官方 Provider 的未知 request field、认证/逐跳 Header、下游客户端标识和网关私有字段不能穿透。
- 先前响应的 HTTP status、stop reason、错误状态或客户端 metadata 不会被反射到后续上游请求；自定义兼容 Upstream 的字段 allowlist 不能越权放行认证和指纹字段。

### 6.7 调度与资源绑定

- 候选过滤顺序、优先级、负载率和 round-robin 在固定时钟下确定可复现。
- semaphore 最后一槽的竞争，失败候选不会超过 max concurrency。
- 429 reset、无 reset、5xx jitter、成功清零和重启后冷却恢复。
- 软亲和不可用后只在同模型候选内逃逸。
- `previous_response_id`、File、Batch、Video 重启后仍命中创建账号。
- Client Key B 获得 Client Key A 的 Sidervia 资源 ID 后仍不能读取、继续、取消或删除。
- 直接提交真实 upstream ID、错误类型 ID、随机 ID、已删除/过期 ID 均不能绕过 Registry。
- 绑定账号 disabled/reauth/cooldown 时返回对应错误，不切换账号。
- 创建上游资源成功但 binding 写入失败时，不向下游泄露不可追踪 ID。

### 6.8 文件、媒体与磁盘

- 客户端文件名含 `../`、绝对路径、NUL、控制字符、超长 Unicode。
- temp 目录中的 symlink/hardlink 竞争、权限、跨文件系统 rename。
- 已知/未知 Content-Length 超限、并发填满 8 GiB、剩余空间保护。
- 正常、取消、上游失败、进程崩溃后的清理。
- 下载端慢读造成背压时，内存保持有界且上游被正确限速/取消。
- 日志、SQLite、core dump 和错误报告不含媒体片段。

### 6.9 用量、价格和审计完整性

- 恶意上游 usage 负数、溢出、NaN、重复字段和不一致总量。
- Price Rule 重叠、空档、未来/历史生效、Batch/priority 条件。
- micro-USD 定点计算、边界舍入和聚合幂等。
- 价格目录发布后不可修改；新版本不改变旧请求金额。
- Audit metadata 拒绝任意 JSON/secret 字段；管理员删除对象后历史事件仍可读。
- 365 天删除前聚合缺失时必须停止清理并告警。

## 7. 模糊测试与性质测试

Go fuzz targets至少包括：

- Client Key parser 和 verifier lookup。
- URL normalization、IP 分类和 redirect policy。
- 原生 JSON 定点修改、Canonical IR codec、错误 mapper。
- SSE event parser、WebSocket JSON event mapper。
- Resource ID codec 和类型检查。
- Provider usage 解析和 Price Rule matcher。

关键性质：

- `decode(encode(valid IR))` 保留声明的共有语义。
- 任意输入不会 panic、无限分配或越过配置大小上限。
- Resource ID decode 成功必然包含合法类型和足够随机熵。
- 金额不为负、不溢出，聚合等于 line items 整数和。
- 调度选择结果一定属于硬过滤后的候选集合。

每个已修复的 parser/状态机安全问题必须留下最小回归 corpus。

## 8. 供应链与发布审计

### 8.1 依赖

- Go module 和前端 lockfile 必须提交，CI 使用 immutable/frozen 安装。
- 新直接依赖需要说明用途、许可证、维护状态和为何标准库不足。
- 禁止未经审查的 install/postinstall 脚本；前端依赖尽量减少。
- Provider SDK 不是默认选择。若原生 HTTP 更简单，避免引入庞大 SDK 依赖树。

### 8.2 GitHub Actions

- 默认 `permissions: contents: read`，每个 job 只提升必要权限。
- 来自 Fork 的 PR 不获得 release、registry 或 signing secrets。
- 第三方 Action 固定完整 commit SHA，并由依赖更新机器人提出可审查变更。
- `pull_request_target` 默认禁止；必须使用时不得 checkout/执行不可信 PR 代码。
- 发布只允许受保护 tag/环境，由人工批准并关联已通过的 commit。

### 8.3 产物

- 二进制包含版本、commit 和 dirty 标记；正式发布必须 `dirty=false`。
- 镜像使用最小运行时、非 root、只读根文件系统兼容，不包含 shell 为目标；若保留 shell 必须有明确运维理由。
- 每个 release 附 SHA-256、SBOM、构建 provenance/attestation 和签名验证说明。
- 从 release 镜像启动测试，而不是只测试源码构建产物。

## 9. 严重级别与修复时限

| 级别 | 示例 | 发布策略 |
| --- | --- | --- |
| Critical | 未授权管理/RCE、明文批量凭证泄漏、任意云元数据 SSRF、发布密钥失陷 | 立即停止发布；已发布则启动事件响应 |
| High | OAuth 串号/重放、跨账号资源访问、持久 XSS、可利用请求走私、主密钥绕过 | 必须修复并独立复测后才能发布 |
| Medium | 有前置条件的 DoS、有限敏感元数据泄漏、不安全默认但有明显告警 | 默认阻断；只有书面接受、负责人和下一小版本期限可例外 |
| Low | 无敏感影响的加固缺口、信息性指纹 | 记录并排入维护计划 |

CVSS 可以作为参考，但最终级别必须结合 Sidervia 持有多上游凭证和可访问付费 API 的实际影响。

## 10. 发布门禁

发布候选必须同时满足：

- 无未解决 Critical/High。
- Medium 例外有公开或内部风险接受记录、负责人、到期版本和补偿控制。
- 自动化门禁全部通过且没有被无说明跳过。
- 安全关键专项测试通过，失败用例有回归测试。
- 完成一次全新部署、升级、备份恢复和错误主密钥演练。
- 扫描实际二进制和镜像，SBOM/签名/校验和存在。
- Provider OAuth 合规状态和验证日期已更新；未知条款不得标为 Stable。
- `SECURITY.md` 的私密报告通道可用。

## 11. 审计证据

每个 release 建立不含秘密的审计清单，至少记录：

```text
release version / commit / image digest
auditor and date
threat-model revision
commands and tool versions
test environment
findings with severity/status
accepted risks and expiration
SBOM/provenance/checksum locations
backup/restore drill result
final release decision
```

公开仓库只发布摘要和已修复问题；含 exploit 细节、token canary、内部网络和未修复漏洞的完整报告保存在受控位置。

## 12. 运行期复核与事件响应

- 每月至少运行依赖、镜像和 secret 扫描；每个 Provider 重大协议/认证变更触发专项复核。
- 管理页面提示长期未验证账号、长期未更新价格和即将过期凭证。
- 发现凭证泄漏：立即禁用账号、在 Provider 侧吊销/轮换、吊销相关 Session/Client Key、保存脱敏证据并检查审计记录。
- 主密钥疑似泄漏：停止实例、轮换所有上游凭证和主密钥，仅数据库重加密不足以消除已泄漏明文风险。
- 发布链失陷：撤销签名/发布 token、标记受影响 digest、从已验证 commit 重建并发布公告。

具体报告渠道和披露流程见仓库根目录 [SECURITY.md](../SECURITY.md)。

## 13. 当前实现覆盖

v0.1 已落地管理认证、加密存储、严格 Admin JSON、控制面审计、备份和容器最小权限的首批控制与回归测试。Provider/OAuth、SSRF transport、协议转换、流式媒体、资源绑定和发布签名尚无实现，因此对应 T-05、T-06、T-09 至 T-15、部分 T-17/T-18 只能保持 planned，不能以“暂无发现”关闭。

逐项代码与证据索引见[实现状态](implementation-status.md)。正式发布仍必须满足第 10 节全部门禁。
