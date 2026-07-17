# Sidervia 直接依赖审查

- 状态：v0.2 复核记录
- 核对日期：2026-07-17
- 锁文件：[`go.sum`](../go.sum)、[`web/pnpm-lock.yaml`](../web/pnpm-lock.yaml)

本文件记录直接依赖的引入理由和许可证判断；实际发布仍以锁文件、SBOM 和当次漏洞扫描为准。未使用的 Provider SDK 不得提前加入。

## Go 运行时依赖

| 依赖 | 锁定版本 | 用途 | 许可证 | 选择理由 |
| --- | --- | --- | --- | --- |
| `modernc.org/sqlite` | `v1.54.0` | SQLite `database/sql` 驱动和在线 Backup API | BSD-3-Clause | 无 CGO，适合静态 amd64/arm64 单容器；避免 ORM |
| `modernc.org/libc` | `v1.74.1`（indirect） | sqlite 驱动要求的 libc 实现 | BSD-3-Clause | 由 sqlite 精确锁定的必要运行依赖 |
| `golang.org/x/crypto` | `v0.54.0` | Argon2id 管理员密码哈希 | BSD-3-Clause | 标准库没有 Argon2id |
| `github.com/pquerna/otp` | `v1.5.0` | RFC 6238 TOTP 生成/验证 | Apache-2.0 | 避免自行实现 OTP 算法和 URI 细节 |

其余 Go 间接依赖由上述模块解析并锁定。应用不引入 Provider SDK、HTTP 框架或 ORM。

## Web 运行时依赖

| 依赖 | 用途 | 许可证 |
| --- | --- | --- |
| React / React DOM | 管理页面渲染 | MIT |
| React Router | SPA 路由 | MIT |
| TanStack Query | Admin API 查询与失效管理 | MIT |
| Radix Dialog / Dropdown Menu | 无样式可访问性 primitives | MIT |
| i18next / react-i18next | `zh-CN` 与 `en` 文案 | MIT |
| Lucide React | 图标 | ISC |
| `qrcode` | 本地生成 TOTP QR 图 | MIT |

Vite、Vitest、TypeScript、ESLint、Testing Library 和 `openapi-typescript` 只用于构建、测试与契约生成，不进入 distroless 运行镜像。

pnpm 11 默认拒绝未审查的依赖安装脚本；[`web/pnpm-workspace.yaml`](../web/pnpm-workspace.yaml) 仅允许 Vite 构建所需的 `esbuild` 脚本。

## 2026-07-17 扫描记录

- `govulncheck v1.1.4 ./...`：0 个可达漏洞、0 个已导入 package 漏洞。
- 模块层报告 `GO-2026-5932`：`golang.org/x/crypto/openpgp` 不再维护且无修复版本；Sidervia 只导入 `argon2`，调用图和 package 扫描均不受影响。后续每次升级继续复核。
- `pnpm audit --prod --audit-level high`：未发现已知生产依赖漏洞。

这些结果只代表该日期、当前锁文件和当前调用图，不替代持续 Dependabot、CI 扫描或 release SBOM。
