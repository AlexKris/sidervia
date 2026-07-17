# Sidervia v0.2 实现状态

- 状态：开发快照，不是生产发行版
- 版本目标：v0.2 First Usable Gateway
- 核对日期：2026-07-17

## 1. 当前可运行范围

仓库已经具备单机控制平面和首个可调用网关实现：

| 领域 | 当前状态 | 主要实现/证据 |
| --- | --- | --- |
| v0.1 Foundation | 已实现 | 管理认证、TOTP/CSRF、SQLite WAL、加密、CRUD、备份、恢复和容器骨架 |
| Client Key | 已实现 | 高熵一次性 secret、不可逆 verifier、状态/到期校验、服务端生成 Request ID |
| Provider API Key | Beta | OpenAI、Anthropic、Google Gemini、xAI 的模型探测和账号激活 |
| 原生文本 JSON/SSE | Beta | 四家 Provider 的纯文本请求、模型重写、受限 SSE 和响应未知字段语义保留 |
| 账号池调度 | 已实现 | 硬过滤、优先级、负载率、轮转、并发槽、429/5xx 冷却和 `Retry-After` |
| 固定出口 | 已实现 | 直连或账号绑定 HTTP/HTTPS/SOCKS5 代理；HTTPS-only、SSRF DNS/IP 复核、禁重定向 |
| Google OAuth | Beta | 标准 Authorization Code + PKCE、state 仅存 verifier、PKCE/token 加密、刷新 singleflight、rotation、401 单次重试 |
| 请求元数据/usage | 已实现基础版本 | 不含正文的请求记录、TTFT/耗时/字节数、Provider 基础 token usage、24 小时汇总、每日基础聚合和 365 天清理 |
| 管理 API/UI | 已实现 v0.2 页面 | 账号验证/运行参数编辑、Google OAuth 配置/连接、多候选账号池、请求列表和 Dashboard；OpenAPI 版本 `0.2.0` |
| 数据升级 | 已实现 | Schema 3、v1→v3 数据保留、OAuth 密文纳入备份验证和主密钥轮换 |

## 2. Provider 与接口边界

| 能力 | OpenAI | Anthropic | Gemini | xAI | OpenAI-compatible |
| --- | --- | --- | --- | --- | --- |
| 原生纯文本非流式/流式 | Beta | Beta | Beta | Beta | planned |
| API Key | Beta | Beta | Beta | Beta | planned |
| OAuth | planned | planned | Google Authorization Code + PKCE Beta | planned | unsupported |
| 工具/Structured Output/Reasoning | planned | planned | planned | planned | planned |
| 图片/文档/音视频 | planned | planned | planned | planned | planned |
| Responses/Files/Batches/Realtime/Live | planned | planned | planned | planned | planned |

当前公开入口：

- OpenAI/xAI：`GET /v1/models`、`POST /v1/chat/completions`。
- Anthropic：`GET /v1/models`、`POST /v1/messages`，协议版本固定为已验证的 `2023-06-01`，不接受未验证 Beta 标头。
- Gemini：`GET /v1beta/models`、`POST /v1beta/models/{model}:generateContent` 和 `:streamGenerateContent`。

“Beta”表示本地协议契约、假上游 HTTP 集成和安全边界测试已通过；不表示已使用维护者的真实付费账号完成官方线上认证，也不表示 Provider 对第三方网关作出兼容承诺。

## 3. 请求与隐私边界

- v0.2 只允许纯文本字符串或纯文本内容块，以及明确列出的采样/安全参数。
- 未知顶层或嵌套字段、客户端 `metadata`/`user`/`status`、工具、媒体和未验证协议版本在出站前被拒绝。
- 下游 Authorization、Cookie、代理头、SDK telemetry、客户端版本和 User-Agent 不进入上游；出站只使用诚实的 `Sidervia/0.2` 标识。
- 原生响应保留 Provider 未知业务字段和 SSE 事件的 JSON 语义，但不会自动把响应状态、stop reason 或 metadata 回声到后续请求。
- 不保存或记录 prompt、response body、tool arguments、Header、API Key、OAuth token/code、代理凭证、文件或媒体内容。
- 浏览器打开 Google 授权页时使用管理员浏览器自身网络；服务端 token 交换、刷新、账号验证和推理使用账号解析出的同一出口。

## 4. 自动化证据

当前测试覆盖：

- v1→v3 migration、外键完整性、SQLite WAL 和中断状态恢复。
- 四家 Provider 的 JSON/SSE 入口、模型重写、认证头注入、下游头剥离和协议错误形状。
- 未知/嵌套字段、伪装在标量控制项中的对象/数组、客户端标识、未验证 Anthropic 版本/Beta、重复 JSON key、深度和 SSE 事件大小边界。
- Client Key 认证、重复下游 Request ID 隔离、请求元数据不含正文。
- 路由硬过滤、Provider/协议一致性、优先级、负载率、轮转、并发、429 reset、5xx 冷却和 OAuth 401 重试。
- Google OAuth state/PKCE、管理 Session/出口绑定、回调重放、scope 固定、token rotation、并发刷新、失效和重启恢复；配置/Attempt 变更与审计同事务回滚。
- 出站私网/DNS 混合答案拒绝、HTTPS-only、禁重定向和账号代理配置。
- OAuth Client Secret、PKCE/provider payload 和账号 token 的备份校验及事务化主密钥轮换。
- 过期请求明细先写入永久基础日聚合，再与清理审计同事务删除；畸形 usage 或审计失败时保留原明细并回滚聚合。
- React 中 API Key/OAuth 表单不向浏览器返回上游凭据，账号运行参数编辑和多候选池提交，OpenAPI 类型、lint、typecheck 和生产构建。

本地门禁：

```bash
make check
make test-race
```

## 5. 尚未满足的 v0.2 发布条件

以下项目仍阻止把当前提交标为正式 v0.2 release：

1. 使用授权的真实 OpenAI、Anthropic、Gemini 和 xAI 测试账号执行线上非流式/流式互通并记录脱敏结果。
2. 在受限为 2 vCPU / 2 GiB 的 Linux 环境完成容量、取消、慢流、连接泄漏和长时间 soak；在此之前推荐 2C4G。
3. 完成独立的安全二次复核、`govulncheck`、生产依赖审计和干净 Linux CI。
4. 构建并验证 amd64/arm64 正式镜像、SBOM、provenance、签名和升级/回滚演练。

Canonical IR、跨协议转换、工具、多模态、状态资源、完整成本目录和 OpenAI-compatible 自定义上游属于 v0.3 以后，不能从当前数据表或页面推断为可用。
