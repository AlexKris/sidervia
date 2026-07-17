# Sidervia 路线图

- 状态：初始实施顺序
- 版本：0.1
- 日期：2026-07-16

路线图按可验证能力分阶段，不承诺日历日期。每个里程碑必须满足自身测试和安全门禁，不能以“后续补测试”作为完成条件。

当前正在实施 v0.1；精确完成项和未实现边界见[实现状态](implementation-status.md)。

## v0.1 — Foundation

目标：建立可安全演进的单机控制平面。

- Go 1.26 项目、React/TypeScript/Vite 前端和嵌入式静态资源。
- 配置校验、进程锁、SQLite WAL、迁移和优雅关闭。
- 主密钥、AES-GCM envelope、管理员密码、Session、CSRF、TOTP。
- Admin API/UI 基础框架和 Client Key。
- Upstream、Proxy、Account、Model Route 的数据模型与 CRUD。
- 假 Provider、CI、race、漏洞和 migration 门禁。
- 一致性备份、`doctor` 和最小 Docker 镜像。

完成定义：管理员能安全登录并配置控制面；尚不宣称可代理真实 Provider。

## v0.2 — First Usable Gateway

目标：提供第一个可实际自托管的文本/流式版本，API Key 与 OAuth 同时可用。

- OpenAI、Anthropic、Gemini、xAI 的 API Key 认证和原生文本/流式核心路径。
- 官方公开且适用于服务端集成的 OAuth/device/WIF 流程；订阅产品适配器标 Beta。
- 账号绑定 HTTP/HTTPS/SOCKS5 代理，登录到推理保持同一出口。
- Model Route、硬过滤、优先级、负载率、轮转、并发和冷却。
- 401 单次刷新重试、token rotation、reauth_required。
- 请求元数据、基础 usage、审计事件和 Dashboard。
- 单机 2C2G 容量测试和部署文档验证。

完成定义：不超过 5 个下游用户可以通过 Client Key 稳定执行核心调用；没有正文/凭证泄漏和 High/Critical 安全问题。

## v0.3 — Protocol Compatibility & Stateful Resources

目标：在不伪造语义的前提下扩展协议兼容和状态资源。

- Canonical IR v1：文本、图片/文档输入、工具、Structured Output、Reasoning 和 usage。
- OpenAI/Anthropic/Gemini 之间已验证的共有语义转换。
- 明确的 capability matrix、warning 和 `capability_not_supported`。
- Responses、Files、Batches 等 Resource Binding 与 ID 重写。
- `previous_response_id` 强绑定、重启恢复和绑定账号错误。
- 完整 usage line items、Price Catalog 版本和 API 等价值。

完成定义：所有声明为 converted-tested 的格子都有双向语义契约测试；状态资源绝不跨账号。

## v0.4 — Native Modalities

目标：逐 Provider 补齐官方原生能力，不进行虚假跨模态转换。

- Embeddings、Files 和 Provider 原生 Batch。
- 图片生成/编辑、音频、视频等官方原生接口。
- OpenAI Realtime、Gemini Live 及 Provider 官方 WebSocket 能力。
- 大文件/媒体流式转发、临时落盘配额和崩溃清理。
- 对应 token、图片、音频、视频、批处理和存储计价单位。
- 每个能力的 Provider 契约、故障、资源和容量测试。

完成定义：能力矩阵区分 native-tested、beta、unsupported；README 不作超出矩阵的宣传。

## v0.5 — Hardening Release Candidate

目标：冻结 v1 公共契约并完成生产加固。

- Admin API、数据库 Schema、Canonical IR 和错误码兼容审查。
- 全量 OWASP ASVS/API Security 审计和独立复测。
- Fuzz corpus、长时间流式 soak、2C2G 容量和故障注入。
- 升级/回滚、主密钥轮换、备份恢复和灾难演练。
- amd64/arm64 镜像、SBOM、provenance、签名和 release 文档。
- Provider 官方协议与 OAuth/条款验证日期刷新。

完成定义：无未解决 Critical/High；所有 Medium 例外都有责任人和到期版本。

## v1.0 — Stable Single-Node

目标：发布稳定的单机、自托管 Sidervia。

- 明确列出的 Provider/协议/能力达到声明的 Stable 或 Beta 状态。
- 管理、路由、资源、用量和备份契约稳定。
- 从最后一个 v0.x 版本原地升级通过。
- 完整运维手册、安全报告摘要和已知限制。
- AGPL-3.0-only、DCO、贡献和漏洞披露流程生效。

v1.0 不意味着支持 SaaS、多租户或多节点。

## v1 之后的评估项

只有真实用户数据证明必要时再设计：

- PostgreSQL/Redis 和多节点调度。
- 多管理员/RBAC/租户隔离。
- 独立对象存储与长期媒体生命周期。
- 稳定、受限且可审计的 Provider 插件 SDK。
- 基于真实观测数据的高级调度评分。

这些不是已承诺功能，也不得在 v1 代码中预埋未使用的抽象。
