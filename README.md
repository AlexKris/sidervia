# Sidervia

> Self-hosted multi-provider AI gateway and control plane with web management, secure routing, protocol compatibility, and usage/cost analytics.

Sidervia 是面向个人和小团队的自托管 AI API 网关与控制平面。目标部署是单台 VPS、单管理员、不超过 5 个下游用户；运行时采用单个 Go 进程、内嵌 React 管理端和 SQLite WAL，不需要 PostgreSQL 或 Redis。

> [!IMPORTANT]
> 当前代码是 **v0.2 开发快照**。四家 Provider 的原生文本/流式核心路径、账号调度和 Google OAuth Beta 已实现并通过本地自动化测试，但尚未完成真实 Provider 凭据互通、2C2G 容量、长时间流式和独立安全复核。它不是生产发行版。

## 当前已实现

- SQLite WAL、只前进迁移、单实例进程锁和优雅关闭。
- 外部主密钥、AES-256-GCM 行列绑定加密、Argon2id、管理员 Session、CSRF 和 TOTP 防重放。
- 同源 Admin API 与中英文 React 管理界面。
- Upstream、Account、Proxy、Model Route 和 Client Key 管理；密钥只显示一次。
- OpenAI Chat Completions、Anthropic Messages、Gemini GenerateContent 和 xAI Chat Completions 的原生纯文本 JSON/SSE Beta 路径。
- API Key 账号验证、模型发现、优先级/负载率/轮转调度、并发槽、429/5xx 冷却和 `Retry-After`。
- 账号绑定 HTTP、HTTPS 或 SOCKS5 代理；出站 DNS/IP 复核、HTTPS-only、禁重定向和诚实的 `Sidervia/0.2` 标识。
- Google 标准 Authorization Code + PKCE OAuth Beta、服务端 token 交换/刷新、rotation、401 单次重试和 `reauth_required`。
- 不含正文的请求元数据、基础 token usage、Dashboard 和管理端请求列表。
- 不记录提示词、响应正文、工具参数或媒体内容的结构化安全日志。
- `doctor`、在线一致性备份/校验、离线密码恢复和主密钥轮换。
- OpenAPI 契约、前端类型生成、Docker/Compose 模板和固定 SHA 的 CI。

当前实现边界和证据见[实现状态](docs/implementation-status.md)。完整产品目标仍以[需求基线](docs/requirements.md)为准。

## 明确边界

- Sidervia 是独立实现，不继承 CLIProxyAPI、Sub2API 或其他网关的源码、Git 历史、Schema、管理 API 或 UI。
- 不伪装官方客户端，不复制客户端指纹，不通过未知字段、Cookie、Session Key 或私有接口规避上游控制。
- 不承诺订阅账号可用于第三方网关；OAuth/device 适配器必须基于官方公开流程并单独核对条款。
- v1 不提供 SaaS 多租户、支付、RBAC、多节点高可用、动态插件或跨模型自动降级。
- 不长期保存提示词、模型响应、文件或音视频；未来媒体路径以流式转发为主。
- v0.2 只接受已验证的纯文本字段和文本内容块；工具、多模态、Structured Output、Reasoning、状态资源和未知请求字段不会盲目透传。

## v0.2 公开接口

| 协议 | 当前接口 | 下游认证 |
| --- | --- | --- |
| OpenAI / xAI | `GET /v1/models`、`POST /v1/chat/completions` | `Authorization: Bearer <Client Key>` |
| Anthropic | `GET /v1/models`、`POST /v1/messages` | `X-Api-Key: <Client Key>` |
| Gemini | `GET /v1beta/models`、`POST /v1beta/models/{model}:generateContent`、`...:streamGenerateContent` | `X-Goog-Api-Key: <Client Key>` |

xAI 通过模型路由选择，不伪装或复制官方客户端标识。请求侧使用递归字段允许列表；响应侧保留 Provider 未知业务字段的 JSON 语义，但未知响应不会进入后续请求。

## 开发与验证

需要 Go 1.26、Node.js 24 和 pnpm 11.13.1：

```bash
make web-install
make check
make test-race
make build
```

前端开发服务器运行 `pnpm --dir web dev`，默认把 `/api`、`/v1` 和 `/v1beta` 代理到 `127.0.0.1:8080`。服务启动需要受限权限的主密钥文件和 bootstrap password file；配置、文件权限与本地启动方法见[部署与运维](docs/deployment.md)。

容器构建：

```bash
docker build -t sidervia:dev .
```

根目录的 `compose.yaml` 要求通过 `.env` 提供固定 digest 的 `SIDERVIA_IMAGE` 和外部 HTTPS `SIDERVIA_PUBLIC_URL`。当前尚无 v0.2 正式镜像，请勿把本地开发镜像视作发布产物。

## 文档

| 文档 | 内容 |
| --- | --- |
| [实现状态](docs/implementation-status.md) | 当前代码、未实现范围和验证证据 |
| [产品需求](docs/requirements.md) | 目标、范围、约束和验收标准 |
| [总体架构](docs/architecture.md) | 系统边界、模块、数据流和部署拓扑 |
| [详细设计](docs/detailed-design.md) | 接口、数据模型、认证、调度、协议和计量设计 |
| [安全审计方案](docs/security-audit.md) | 威胁模型、审计方法和发布门禁 |
| [测试方案](docs/testing.md) | 单元、集成、兼容、故障与性能测试矩阵 |
| [依赖审查](docs/dependencies.md) | 直接依赖用途、许可证和当前漏洞扫描记录 |
| [部署设计](docs/deployment.md) | 容量、Docker、备份、升级和监控 |
| [路线图](docs/roadmap.md) | v0.x 到 v1.0 的分阶段交付 |
| [参考声明](REFERENCES.md) | 灵感来源和独立实现边界 |

## 服务器资源

对不超过 5 个用户、无本地媒体归档的最终单机版本：

- 2 vCPU / 2 GiB RAM / 40 GiB SSD：设计目标，尚未通过 v0.2 容量验收。
- 2 vCPU / 4 GiB RAM / 40 GiB SSD：容量验收完成前的推荐配置，也更适合较多并发流或同机诊断。
- 生产 VPS 应拉取预构建镜像，不在服务器上构建前后端。

## License

Sidervia 采用 [GNU Affero General Public License v3.0 only](LICENSE)（`AGPL-3.0-only`）。贡献需遵循 [DCO 1.1](CONTRIBUTING.md#developer-certificate-of-origin)。

安全问题请按 [SECURITY.md](SECURITY.md) 私密报告，不要在公开 Issue 中提交凭证、请求正文或未修复漏洞。
