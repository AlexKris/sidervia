# Sidervia v0.1 实现状态

- 状态：开发中，不是生产发行版
- 版本目标：v0.1 Foundation
- 核对日期：2026-07-17

## 1. 可运行范围

当前仓库已经形成可构建的单机控制平面，而不再只是设计文档：

| 领域 | 当前状态 | 主要实现/证据 |
| --- | --- | --- |
| 配置与进程 | 已实现 | 生产 HTTPS 校验、主密钥必填、单实例锁、SIGTERM 有界关闭 |
| SQLite | 已实现 | 文件数据库、WAL、foreign keys、busy timeout、`BEGIN IMMEDIATE`、迁移 checksum、只读检查 |
| 凭证保护 | 已实现 | 外部 32-byte 主密钥、AES-256-GCM、per-row AAD、加密 sentinel |
| 管理认证 | 已实现 | Argon2id、Session Cookie、CSRF、Origin/Referer、TOTP 防重放、登录锁定 |
| 控制面 | 已实现基础 CRUD | Proxy、Upstream、API Key Account、Model Route、Client Key、审计事件 |
| 管理页面 | 已实现基础版本 | `zh-CN`/`en`、响应式页面、一次性 secret、TOTP 和密码设置 |
| 维护 | 已实现 | `doctor`、在线 SQLite Backup API、SHA-256 校验、离线 key rotation/password reset |
| 接口契约 | 已实现 | `api/admin.openapi.yaml` 与生成的 TypeScript 类型 |
| 交付骨架 | 已实现 | 多阶段 Dockerfile、distroless non-root、Compose、GitHub Actions、Dependabot |

所有 Account 在 v0.1 仅能保存为 `draft`/`disabled` 配置；没有验证成功或进入真实调度池的路径。

## 2. 明确未实现

以下仍是后续里程碑，不能从已有页面或数据表推断为可用：

- OpenAI、Anthropic、Gemini、xAI 或 OpenAI-compatible 的真实网络调用。
- Provider API Key 验证、OAuth/device/WIF、token refresh 与账号出口代理。
- Model Route 调度、并发槽、冷却、软亲和和强资源绑定。
- Native codec、Canonical IR、SSE/WebSocket/媒体转发。
- 请求用量、成本、Price Catalog、长期聚合和 Dashboard 趋势。
- Files/Batches/Responses/Video 等资源生命周期。
- amd64/arm64 正式发布、SBOM、provenance、签名与完整安全发布门禁。
- 2C2G 的流式容量、故障恢复和长时间 soak 验收。

公开协议命名空间不会回退到 React 页面，也不会伪造成功；当前统一返回 HTTP 501 和稳定的安全错误 envelope。

## 3. 当前自动化证据

已覆盖的测试类别：

- 配置边界、文件权限、进程锁、迁移、WAL 和只读打开。
- 主密钥解析、AES-GCM AAD/篡改、sentinel 和密钥轮换。
- 密码、TOTP 窗口与重放、Session/CSRF/Origin、双维登录限速和 Argon2 并发上限。
- Admin JSON 严格解析、安全 Header、Cookie、ETag/If-Match 和 CRUD 审计。
- Client Key 一次性明文、不可逆 verifier、禁用/吊销。
- 备份创建、checksum 篡改检测和恢复密钥验证。
- 前端 CSRF 传递、401 处理、登录 secret 不进入持久存储、lint/typecheck/build。
- 本机 arm64 完整 distroless 镜像构建、UID/GID `65532:65532`、只读容器启动、在线备份/校验，以及从隔离恢复卷重新启动并通过 readiness。

本地基础门禁：

```bash
make check
make test-race
```

CI 另外执行 `govulncheck`、前端生产依赖审计、OpenAPI 类型漂移检查和容器构建/非 root smoke test。当前扫描结果见[依赖审查](dependencies.md)。完整 Provider、性能和发布安全矩阵只有对应代码出现后才可转为通过状态。

## 4. v0.1 完成条件

在标记 v0.1 完成前仍需：

1. 让全量本地/CI 门禁在干净 Linux runner 上通过。
2. 对当前安全关键代码做独立二次审查，并记录发现状态。
3. 确认开发分支无明文真实 secret/body 和 High/Critical 可达漏洞。

v0.1 完成仍不代表真实 AI 请求可用；第一个可调用版本属于 v0.2。
