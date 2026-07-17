# Sidervia 部署与运维设计

- 状态：生产部署基线
- 版本：0.1
- 日期：2026-07-17

> 当前仓库处于 v0.1 Foundation，尚无生产发行镜像，也不具备真实 Provider 转发。本文是最终单机版本的部署基线；当前可验证范围见[实现状态](implementation-status.md)。

## 1. 推荐结论

不超过 5 个下游用户、文件和媒体仅流式转发时：

- **2 vCPU / 2 GiB RAM / 40 GiB SSD**：可用基线。
- **2 vCPU / 4 GiB RAM / 40 GiB SSD**：更适合较多并发流、Realtime/Live、批处理管理或偶尔在服务器执行诊断。
- 不需要 PostgreSQL、Redis、消息队列或独立 Node 服务。
- 生产 VPS 拉取 GitHub Container Registry 的预构建镜像，不在 VPS 构建 Go/React。

实际瓶颈通常先出现在上游限流、网络带宽、连接数和临时媒体空间，而不是 CPU。

## 2. 支持的部署形态

v1 只支持单实例：

```text
Internet / trusted clients
        |
      HTTPS
        |
Caddy / existing reverse proxy
        |
  127.0.0.1:8080
        |
  Sidervia (one process)
        |
SQLite + WAL on local persistent SSD
```

明确禁止：

- 两个 Sidervia 实例同时打开同一数据目录。
- 把 SQLite 放在 NFS、SMB、对象存储挂载或多节点共享卷。
- 通过复制容器副本实现“高可用”。
- 暴露无 TLS 的管理页面到公网。

## 3. 容量预算

### 3.1 CPU 与内存

典型预算：

| 项目 | 2 GiB 主机预算 |
| --- | ---: |
| OS、Docker、Caddy | 300–500 MiB |
| Sidervia 稳态目标 | 256–512 MiB |
| SQLite page cache / 文件缓存 | 256–512 MiB |
| 流式缓冲、TLS 和临时峰值 | 256–512 MiB |
| 安全余量 | 256 MiB 以上 |

2 GiB 能够运行，但不适合在同机执行前端构建、容器镜像构建或其他重服务。建议配置 1–2 GiB swap 作为 OOM 缓冲；swap 不能替代内存，也不应承载持续工作集。

### 3.2 磁盘

40 GiB 建议预算：

| 用途 | 上限/告警 |
| --- | ---: |
| OS、Docker 和镜像 | 8–12 GiB |
| SQLite、WAL 和索引 | 10 GiB 软告警 |
| Sidervia 临时文件 | 默认硬上限 8 GiB |
| 轮转日志 | 2 GiB |
| 本机短期备份 | 5–8 GiB，随后移出 VPS |
| 必须保留的空闲空间 | 至少 5 GiB |

在 1,000 请求/日量级，365 天不含正文的明细通常远低于该预算；在 10,000 请求/日以上必须根据真实行大小重新估算，不能继续假设 40 GiB 足够。

### 3.3 带宽

文本流量通常较小，图片/音频/视频和文件转发会双向消耗 VPS 带宽。因为 Sidervia 不长期归档媒体，磁盘压力受控，但出口流量和并发连接仍应纳入 VPS 套餐。

## 4. 镜像与版本

发布目标：

```text
ghcr.io/alexkris/sidervia:<semver>
ghcr.io/alexkris/sidervia:<semver>@sha256:<digest>
```

生产 Compose 必须固定 semver，推荐同时固定 digest。不要部署 `latest`。

镜像要求：

- linux/amd64 与 linux/arm64。
- 前端已嵌入 Go 二进制。
- 非 root 用户、默认只读根文件系统。
- 运行层不包含 Node、Go toolchain、Git、源码或测试 fixture。
- 发布 SBOM、SHA-256、构建 provenance 和签名验证说明。

## 5. 目录与秘密

主机建议结构：

```text
/opt/sidervia/
├── compose.yaml
├── Caddyfile
├── secrets/
│   ├── master.key           # 容器部署时归 UID/GID 65532 所有
│   └── bootstrap-password   # 同上；首次成功后删除
└── backup/                  # 短期暂存，尽快复制到异机

/var/lib/docker/volumes/...  # Sidervia data volume
```

生成主密钥示例：

```bash
umask 077
openssl rand -base64 32 > /opt/sidervia/secrets/master.key
chmod 600 /opt/sidervia/secrets/master.key
chown 65532:65532 /opt/sidervia/secrets/master.key
```

要求：

- 主密钥和数据库备份分开保存。
- 主密钥至少有一份离线/异机备份；丢失后凭证不可恢复。
- 容器以 UID/GID 65532 运行时，secret 目录和文件必须允许该 UID 读取；推荐目录 0700、文件 0400/0600，且不得对 group/other 开放。
- bootstrap password 使用密码管理器生成，设为 UID/GID 65532 所有，首次创建管理员后删除主机文件和 Compose 挂载。
- 不把 secret 写进 `.env`、Compose、shell history、Git 或容器镜像。

## 6. Compose 模板

仓库根目录的 [`compose.yaml`](../compose.yaml) 是受版本控制的唯一模板。复制 [`.env.example`](../.env.example) 为 `.env`，填写：

- `SIDERVIA_IMAGE`：必须是 `semver@sha256:digest` 形式的正式镜像引用。
- `SIDERVIA_PUBLIC_URL`：管理员实际访问的外部 HTTPS origin。

模板以 UID/GID 65532、只读根文件系统、仅主机回环端口、全部 capability drop、有界日志和 tmpfs 运行。可先执行以下命令检查插值，不会启动容器：

```bash
docker compose config --quiet
```

首次启动成功后，从 `compose.yaml` 移除 `SIDERVIA_BOOTSTRAP_PASSWORD_FILE` 环境项和 bootstrap password 挂载，删除主机文件，再重新创建容器。不要只删除文件而保留一个失效挂载。

容器 `/tmp` 只放小型进程临时数据；需要 seek/replay 的大媒体临时文件位于受 Sidervia 配额管理的数据卷子目录。

## 7. TLS 与反向代理

- [`deploy/Caddyfile.example`](../deploy/Caddyfile.example) 提供最小同机 Caddy 起点；先替换域名，再使用 `caddy validate` 检查。
- 使用 Caddy、Nginx 或现有可信入口终止 TLS。
- Sidervia 默认绑定 `127.0.0.1`；容器场景只映射到主机回环地址。
- `SIDERVIA_PUBLIC_URL` 必须与管理员访问和 OAuth redirect 的外部 HTTPS URL 完全一致。
- 只有反向代理地址进入 `SIDERVIA_TRUSTED_PROXIES`。不要使用 `0.0.0.0/0`。
- 反向代理必须支持 SSE flush 和 WebSocket Upgrade，不应缓存公开 API/管理响应。
- 请求体上限应与 Sidervia 接口限制一致；反向代理更小的限制会先返回 413，这是可接受行为。
- 管理页面设置 HSTS、CSP、frame-ancestors 等 Header 时由 Sidervia 和入口协调，避免互相覆盖成更弱配置。

如果管理页面不需要公网访问，优先通过 WireGuard/Tailscale/SSH tunnel 访问，而不是仅依赖隐藏路径。

## 8. 防火墙与网络

主机入站：

- 22/tcp：只允许管理来源，优先密钥登录。
- 80/443：Caddy 及 ACME。
- 8080 和 metrics 端口：不得公网开放。

出站：

- 允许所配置 Provider、OAuth IdP、DNS、时间同步和必要代理。
- 企业防火墙可做域名 allowlist，但应用内 SSRF 校验仍必须保留。
- 使用账号代理时，登录、刷新、quota 和推理都通过同一代理；不要只代理推理请求。

## 9. 首次部署

1. 创建 VPS、系统用户、Docker/Compose 和 TLS DNS。
2. 创建 `/opt/sidervia`、master key 和 bootstrap password，权限 0700/0600。
3. 填写固定 digest 的 Compose 和反向代理配置。
4. 启动 Sidervia，检查容器健康和 `/readyz`。
5. 登录管理页面，立即启用 TOTP。
6. 删除 bootstrap password 文件与 Compose 挂载，重建容器并再次登录。
7. v0.2 及以后添加测试 Provider/Account/Client Key，执行非流式和 SSE 请求；v0.1 只验证控制面与明确的 501 响应。
8. 创建首份一致性备份，复制到异机并执行恢复验证。
9. 配置日志轮转、磁盘/内存/证书/备份告警。

不得在完成第 8 步之前批量导入真实上游凭证。

## 10. 备份与恢复

### 10.1 备份内容

- Sidervia 一致性 SQLite 备份。
- 当前 release 版本、镜像 digest 和配置（不含 secret）。
- 主密钥的独立加密备份。
- 必要的反向代理配置和恢复说明。

临时媒体目录、日志和镜像 layer 不进入业务备份。

### 10.2 创建备份

首选：

```bash
docker compose exec sidervia \
  /sidervia backup create --output /var/lib/sidervia/backup/sidervia-TIMESTAMP.db
```

该命令使用 SQLite Backup API 并在完成后执行完整性检查。不要在服务运行时只复制主 `.db` 文件而忽略 WAL。

如果内置命令不可用，停止容器后复制完整数据目录；确认进程已退出再复制。

### 10.3 频率与保留

- 每日自动备份，至少保留 7 份日备份和 4 份周备份。
- 数据库和 master key 至少一份异机保存。
- 每月至少执行一次恢复演练，不能只验证文件存在。
- 备份文件按实际敏感数据处理，即使凭证已加密；权限 0600，并使用传输/存储加密。

### 10.4 恢复

1. 在隔离目录验证备份 checksum 和 `backup verify`。
2. 停止 Sidervia，保留故障数据目录副本。
3. 放置数据库和正确主密钥，确保 UID/权限匹配。
4. 使用与备份版本相同的镜像先启动并通过 `/readyz`。
5. 再按正常升级流程升级，不跨多个未知版本直接尝试。
6. 验证管理员登录、账号解密、资源 binding、请求历史和一条测试调用。

## 11. 升级与回滚

升级：

1. 阅读 release notes、Schema 变更和已知 Provider 变化。
2. 拉取并验证新 digest、签名和 SBOM。
3. 创建并验证备份。
4. 在 staging 或备份副本上运行 `doctor` 和迁移演练。
5. 停止旧容器，更新固定 digest，启动新容器。
6. 检查迁移、`/readyz`、Dashboard、日志和测试调用。

回滚不是简单切回旧镜像：旧二进制可能不理解新 Schema。发生不可逆迁移后，必须停止新容器、恢复升级前数据库备份，再启动旧 digest。

自动更新工具不得跳过备份与迁移检查。生产默认手动批准升级。

## 12. 日志与监控

### 12.1 日志

- 使用 Docker `local`/`json-file` 有界轮转或集中式日志系统。
- 单机建议总日志上限 2 GiB。
- 不启用 HTTP body dump、TLS key log 或第三方反向代理完整 query 日志。
- Debug 日志也不得包含凭证或正文；临时提升级别后及时恢复。

### 12.2 告警

至少监控：

- `/readyz`、容器重启和异常退出。
- 内存 > 80%、磁盘可用 < 8 GiB/5 GiB、inode。
- SQLite busy/WAL 持续增长、Usage queue backpressure。
- 5xx/429/401、reauth_required、全部账号不可用。
- 临时目录用量和清理失败。
- TLS 证书到期、备份过期和恢复演练过期。

Prometheus `/metrics` 只监听本机/私网，不经公网域名公开。

## 13. SQLite 维护

- WAL checkpoint 由应用协调，不使用外部 cron 在运行时直接执行 SQLite 写命令。
- `VACUUM` 需要额外磁盘空间和维护窗口；40 GiB 主机先检查至少数据库大小 1–2 倍空闲空间。
- 请求明细清理由应用执行并确认聚合存在。
- 不手工编辑 Account credential、Resource Binding 或 migration 表。
- `doctor` 报告完整性、Schema、WAL、磁盘、key sentinel 和需要维护的项目，但不输出秘密。

## 14. 故障处理

### 所有账号 401/reauth

暂停公开调用，检查 Provider 状态和系统时间；通过管理页面逐个重新认证。不要批量复制 token 或修改数据库。

### 持续 429

检查官方 reset、实际并发和 Model Route；不要清空 `account_runtime` 强行绕过冷却。需要更多吞吐时增加官方授权渠道或降低并发。

### 磁盘不足

停止新的大文件请求，清理已确认过期的临时文件、旧镜像和轮转日志；不要直接删除 SQLite WAL。恢复到 5 GiB 以上再执行维护。

### 主密钥丢失

若没有备份，凭证不可恢复。创建新实例并重新添加所有账号；不存在“重置密钥后读取旧密文”的安全后门。

### 数据库损坏

停止实例、保留故障文件用于分析，从最近已验证备份恢复。不得在唯一副本上反复运行破坏性修复。

## 15. 扩容判断

以下任一情况持续出现时，应重新评估架构，而不是盲目增加容器副本：

- 超过 20–50 个长期并发流。
- 请求明细增长使 40 GiB 磁盘或 SQLite 查询持续受压。
- 单机故障恢复时间不满足业务要求。
- 需要多个管理员、租户隔离或区域部署。

届时需要新设计 PostgreSQL/Redis/对象存储和分布式调度；它们不是当前 v1 的隐藏可选开关。
