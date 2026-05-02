# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Stage 4 (quality): tests in `internal/pipeline`, `internal/platform/iox`, `internal/platform/sqlpool`; `ExampleParser_Parse_githubPing` in `internal/webhook`; stable `test-cover-check` coverage floor (≥29% with `go test -short`).

### Changed

- A+ documentation: README (Makefile table aligned with `fmt`/goimports, `validate-full`, `test-cover-check`, `docs/` index), CONTRIBUTING (Conventional Commits), SECURITY (`Cache-Control` header), new `docs/deployment.md` and expanded `docs/architecture.md` / `docs/api.md` / `docs/setup.md`.
- `internal/proxy`: reuse one `http.Transport` per proxy instance; `Makefile` `test-cover-check` uses a safer flag order for shells.
- `.golangci.yml`: linter set aligned with the plan (with exclusions documented in `PLAN_PROGRESS.md`).
- Project copy and operator-facing strings internationalised to English (`README`, `docs/`, errors, logs).

### Fixed

- Godoc: `ResourceConflictError` type and `Error` method in `scheduler`; clarified `integrationtest` package comment.
- Proxy hot-reload test and connection leaks in `ServeHTTP`; `godeployd` exits via return code to honour defers; Docker-backed observability tests respect `-short`.

## [0.1.0] - 2026-05-01

### Added

- Multi-binary layout: `godeployd` (HTTP daemon), `godeploy-tui`, `godeploy-logtail`.
- Pipeline: git shallow clone, runtime detection, Docker image build, blue-green style deploy, SQLite route updates.
- Reverse proxy package with SQLite-backed routes and hot reload.
- Webhook parsing for GitHub (`X-Hub-Signature-256`) and GitLab (`X-Gitlab-Token`) push events.
- Observability: JSON stats endpoint and WebSocket container log streaming.
- Middleware: security headers, webhook rate limiting, restrictive WebSocket origins.
- `deployments/` Dockerfile and Compose for running the daemon with a mounted Docker socket.
- CI workflow: format check, vet, short tests, build, golangci-lint, govulncheck (non-blocking where upstream has no fix).

### Security

- Documented dependency scanning notes and operational mitigations in `SECURITY.md`.

[Unreleased]: https://github.com/esousa97/godeploy-platform/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/esousa97/godeploy-platform/releases/tag/v0.1.0
