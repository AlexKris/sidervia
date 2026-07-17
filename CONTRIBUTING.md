# Contributing to Sidervia

Sidervia is security-sensitive: the current control plane stores credentials, and later milestones will forward paid API traffic. Keep changes small, traceable and independently implemented.

## Before contributing

- Read [requirements](docs/requirements.md), [architecture](docs/architecture.md), [detailed design](docs/detailed-design.md) and [reference rules](REFERENCES.md).
- Open an Issue before changing a public API, database Schema, Canonical IR, authentication flow, routing algorithm or security boundary.
- Do not include production credentials, request/response bodies, private URLs or customer data in Issues, tests, fixtures or logs.
- Do not copy or mechanically port code, UI, schemas, tests, fixtures or prose from CLIProxyAPI, Sub2API or other projects.

## Development rules

- Prefer simple, maintainable changes. Do not add speculative abstractions or compatibility layers.
- Every behavior change must map to a requirement/design decision and include tests.
- Provider behavior must cite official public documentation and update its capability verification date.
- Native support and converted support must remain distinct; do not silently drop unsupported semantics.
- Security-sensitive failures default closed and must use stable, non-secret error codes.
- New dependencies require a clear reason, license check and vulnerability review.

## Commit convention

Use Conventional Commits:

```text
feat: add account validation state
fix: bind response resources to creating account
docs: clarify OAuth retry boundary
test: cover refresh token rotation race
chore: update release workflow
```

Keep commits focused. Do not include generated debug output or AI attribution text.

## Developer Certificate of Origin

All commits must be signed off under the [Developer Certificate of Origin 1.1](DCO):

```bash
git commit -s -m "feat: add provider capability registry"
```

This adds a line like:

```text
Signed-off-by: Your Name <you@example.com>
```

The sign-off certifies that you have the right to submit the contribution under this project's license. It is not a GPG signature.

## Verification

Run the checks relevant to the change. The backend baseline is:

```bash
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

Frontend changes must pass the locked package manager's format/lint, typecheck, unit tests and production build. Protocol, migration, OAuth, routing and security changes also require their integration/fuzz tests described in [testing.md](docs/testing.md).

Do not mark a PR ready while required checks are skipped or flaky. Explain any environment-dependent check with reproducible evidence.

## Pull request checklist

- [ ] Change is scoped to one problem and follows current design.
- [ ] Tests reproduce the behavior/failure and pass after the change.
- [ ] Public/API/Schema/security changes update the relevant docs.
- [ ] No secret, body content, private endpoint or copied third-party artifact is included.
- [ ] New dependency has purpose, license and vulnerability review.
- [ ] Commits include a DCO `Signed-off-by` line.
- [ ] Security impact and rollback/migration behavior are documented.

## License

Contributions are accepted under the repository license, `AGPL-3.0-only`. Third-party code must be explicitly identified and license-compatible; do not paste third-party snippets without provenance and review.
