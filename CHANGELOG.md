# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Etapa 4 (qualidade): testes em `internal/pipeline`, `internal/platform/iox`, `internal/platform/sqlpool`; exemplo `ExampleParser_Parse_githubPing` em `internal/webhook`; piso de cobertura `test-cover-check` estável (≥29% com `go test -short`).

### Changed

- Documentação nível A+: README (tabela Makefile alinhada a `fmt`/goimports, `validate-full`, `test-cover-check`, secção índice `docs/`), CONTRIBUTING (Conventional Commits), SECURITY (header `Cache-Control`), novos `docs/deployment.md` e expansão de `docs/architecture.md` / `docs/api.md` / `docs/setup.md`.
- `internal/proxy`: reutilização de um `http.Transport` por instância do proxy; `Makefile` `test-cover-check` usa ordem de flags mais segura para shells.
- `.golangci.yml`: conjunto de linters alinhado ao plano (com exclusões/ajustes documentados em `PROGRESSO_etapas.md`).

### Fixed

- Godoc: tipo `ResourceConflictError` e método `Error` no pacote `scheduler`; comentário de pacote `integrationtest` clarificado.
- Teste de hot-reload do proxy e fugas de ligações em `ServeHTTP`; `godeployd` com saída por código de retorno para respeitar defers; testes de observability com Docker respeitam `-short`.

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
