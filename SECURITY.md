# Security Policy

Sidervia is designed to handle upstream API credentials and paid API traffic; the current pre-release control plane already stores encrypted credential configuration. Please report vulnerabilities privately and avoid exposing credentials or exploit details in public Issues.

## Supported versions

Sidervia is currently pre-release and has no supported production version. Until v1.0, security fixes target the latest development branch and the newest published v0.x release when one exists.

After v1.0, this table will list maintained release lines and end-of-support dates.

## Reporting a vulnerability

Preferred channel: use GitHub Private Vulnerability Reporting at:

<https://github.com/AlexKris/sidervia/security/advisories/new>

If GitHub reports that private reporting is unavailable, do not open a public Issue with exploit details. Contact the maintainer through the [AlexKris GitHub profile](https://github.com/AlexKris) and request a private reporting channel.

Include when possible:

- Affected version, commit or image digest.
- Vulnerability class and realistic impact.
- Minimal reproduction using fake/test credentials.
- Relevant configuration with all secrets redacted.
- Whether the issue may already be exploited.
- Suggested mitigation, if known.

Never send a real access token, refresh token, API key, Client Key, master key, prompt/response body or private customer data. Replace secrets with recognizable canaries and revoke any credential accidentally exposed.

## Response targets

- Acknowledge receipt within 3 business days.
- Provide initial severity/triage within 7 business days.
- Coordinate remediation and disclosure based on impact; Critical issues take priority over normal release work.

These are targets, not a paid support SLA.

## Scope

In scope:

- Authentication, authorization, CSRF/session and Client Key handling.
- OAuth/device flow, token refresh/rotation and credential encryption.
- SSRF, DNS rebinding, proxy/TLS and unsafe upstream response handling.
- Protocol conversion, resource binding, request smuggling and streaming safety.
- Secret/log/media leakage, SQLite/backup and container/release supply chain.

Out of scope unless caused by Sidervia:

- Vulnerabilities in an upstream Provider itself.
- Provider policy/availability disputes without a technical Sidervia flaw.
- Denial of service requiring unbounded traffic when documented deployment limits are already enforced.
- Social engineering, physical attacks or attacks against third-party accounts without permission.

## Safe research

- Test only systems and accounts you own or are authorized to assess.
- Prefer local fake Providers and staging credentials.
- Do not access other users' data, persist access, disrupt production or incur third-party charges.
- Stop and report if you encounter real credentials or private content.
- Give maintainers reasonable time to fix before public disclosure.

Good-faith research following these rules will not be intentionally pursued by the project maintainers, but this statement cannot authorize testing against third-party Provider infrastructure.

The detailed project audit and release gates are documented in [docs/security-audit.md](docs/security-audit.md).
