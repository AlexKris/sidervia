# References and Independent Implementation Notice

Sidervia is a new, independently implemented project. It is not a fork of any existing gateway and does not inherit another project's Git history, source code, package structure, database schema, management API, configuration format, UI, tests, fixtures, names, or assets.

## Architectural references

The following public projects helped frame the problem space:

### CLIProxyAPI

- Project: [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)
- License observed in the referenced repository: MIT
- High-level ideas studied: a lean Go multi-provider gateway, provider-specific transports, protocol translation boundaries, OAuth-capable accounts, and request usage hooks.

### Sub2API

- Project: [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api)
- License observed in the referenced repository: GNU Lesser General Public License version 3 text
- High-level ideas studied: web-based account administration, account lifecycle, concurrency-aware routing, resource persistence, usage/cost reporting, and operational failure handling.

These observations are architectural context only. No commit history or upstream Git remote from either project is used by Sidervia. Sidervia's only expected project remote is its own `origin`.

## Independent implementation rule

Contributors may read public projects to understand the ecosystem, but Sidervia code and artifacts must be written from:

1. Sidervia's requirements and design documents.
2. Provider-owned public protocol documentation.
3. Independently created interoperability tests and black-box observations made with authorization.
4. General engineering knowledge and permissively licensed dependencies recorded through normal package management.

Do not copy or mechanically port source, UI, migrations, schemas, API handlers, comments, tests, fixtures, documentation prose, icons, screenshots, or generated assets from reference projects. “Renaming symbols” is not independent implementation.

Sidervia is not presented as a strict legal clean-room effort because maintainers may have inspected reference source. The practical rule is original implementation with traceable design decisions and no copied protected expression.

If a contribution intentionally incorporates third-party code, it must be isolated, retain all required notices, be compatible with `AGPL-3.0-only`, and receive explicit maintainer review before merge.

## Provider protocol sources

Provider adapters are based on official documentation, including:

- OpenAI: [API reference](https://developers.openai.com/api/reference/overview), [Responses](https://developers.openai.com/api/reference/responses/overview), [Realtime](https://developers.openai.com/api/docs/guides/realtime)
- Anthropic: [API overview](https://platform.claude.com/docs/en/api/overview), [Authentication](https://platform.claude.com/docs/en/manage-claude/authentication), [Messages](https://platform.claude.com/docs/en/api/messages)
- Google Gemini: [Gemini API](https://ai.google.dev/gemini-api/docs), [OAuth](https://ai.google.dev/gemini-api/docs/oauth), [Live API](https://ai.google.dev/gemini-api/docs/live-api/capabilities)
- xAI: [Inference REST API](https://docs.x.ai/developers/rest-api-reference/inference), [Enterprise authentication](https://docs.x.ai/build/enterprise), [Batch API](https://docs.x.ai/developers/advanced-api-usage/batch-api)

Protocol support and prices change. Each adapter and price catalog must record its own verification/retrieval date rather than treating this file as a frozen specification.

## Trademark statement

OpenAI, Codex, Claude, Anthropic, Gemini, Google, Grok, xAI, and other names are trademarks of their respective owners. Their mention describes interoperability only and does not imply endorsement or affiliation.
