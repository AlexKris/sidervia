# Sidervia Repository Instructions

## Project scope

- Sidervia is an independent implementation. Do not copy or mechanically port source, UI, schemas, APIs, tests, fixtures, comments, assets, or Git history from CLIProxyAPI, Sub2API, or other gateways.
- Use provider-owned public documentation and independently written interoperability tests as protocol sources.
- The current product target is a single-node, single-admin, self-hosted gateway for fewer than five downstream users.
- Do not introduce PostgreSQL, Redis, message queues, dynamic plugins, SaaS users, RBAC, billing, or media archival unless the requirements are explicitly changed first.

## Sources of truth

Read these before implementation:

1. `docs/requirements.md`
2. `docs/architecture.md`
3. `docs/detailed-design.md`
4. `docs/security-audit.md`
5. `docs/testing.md`

If code and docs disagree, stop and resolve the design decision instead of silently changing behavior.

## Engineering

- Backend: Go 1.26. Frontend: React, TypeScript, Vite. Storage: SQLite WAL.
- Keep Provider auth, transport, native codecs, Canonical IR, routing, resources, and usage as separate focused packages.
- Preserve native protocol fields only within the documented security boundary. Never impersonate official clients or add bypass behavior.
- Never persist or log prompts, response bodies, tool arguments, media content, access/refresh tokens, API keys, Client Keys, proxy credentials, Session cookies, or TOTP secrets.
- Use `apply_patch` for intentional file edits. Keep changes minimal and scoped.
- Use Conventional Commits and DCO sign-off. Do not commit or push unless the user asks.

## Verification

- Every behavior change needs tests; bug fixes should first reproduce the failure.
- Run the smallest relevant checks during iteration and the full documented gate before declaring completion.
- Security-sensitive changes require the matching cases in `docs/security-audit.md` and `docs/testing.md`.
- Do not claim a Provider capability until its official source, verification date, and contract test exist.
