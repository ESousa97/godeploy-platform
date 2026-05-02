# Security policy

## Reporting a vulnerability

Please report security issues **responsibly**:

- Prefer **[GitHub Security Advisories](https://github.com/esousa97/godeploy-platform/security/advisories/new)** for this repository (private disclosure to maintainers).
- If that channel is unavailable, open a **public issue with minimal detail** (no exploit steps, no sensitive data) and ask to be contacted privately; the maintainer will follow up via GitHub.

Do not post working exploits or production credentials in public issues.

## Response expectations

- **First acknowledgement**: best effort within **7 days** of a valid report via GitHub Security Advisories.
- **Status updates**: when a fix is planned or released, or if the report is declined as out of scope.
- This is a **study / side project**; timelines depend on maintainer availability and are not a commercial SLA.

## Supported versions

Security fixes are applied on the **default branch** (`main`) going forward. Tagged releases, when published, document the recommended version in [CHANGELOG.md](CHANGELOG.md). Older tags may not receive backports.

## Dependency scanning

The Go client `github.com/docker/docker` is kept on the **latest version compatible** with this codebase; `govulncheck` may still list findings (GO-2026-4887, GO-2026-4883) with *Fixed in: N/A* in the vulnerability database, because the analysis engine associates the module with Moby/daemon code paths.

This repository uses the package **only as an HTTP client** for the Docker Engine API (images, containers, networks). Vectors described in those advisories target **daemon** components (AuthZ, plugins), not the `godeployd` binary. Still, run `govulncheck ./...` on each release and keep Docker Engine updated on the host.

Mitigations in `godeployd` and related services:

- `/webhook`: **body limit** (`http.MaxBytesReader`), **rate limit** per IP (`GODEPLOY_WEBHOOK_RPS` / `GODEPLOY_WEBHOOK_BURST`), **no internal detail leakage** on 500 responses.
- **HTTP security headers** (nosniff, frame deny, referrer-policy, permissions-policy, `Cache-Control: no-store` on `godeployd`).
- **WebSocket** `/api/ws/logs`: restrictive `CheckOrigin` (same host + optional `GODEPLOY_WS_ALLOWED_ORIGINS`); stream errors without daemon internals.
- **Proxy** (`internal/proxy`): timeouts on `http.Server` and upstream `Transport`.
- **Pipeline**: health-check HTTP client uses an explicit-timeout `Transport`.
- Structured logging with **`log/slog`** in the daemon and pipeline.

If you believe there is an exploitable issue in this repository, please report it responsibly using the instructions above.
