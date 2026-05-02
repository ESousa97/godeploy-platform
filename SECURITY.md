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

O cliente Go `github.com/docker/docker` é mantido na **versão mais recente compatível** com o código; `govulncheck` ainda pode listar avisos (GO-2026-4887, GO-2026-4883) com *Fixed in: N/A* no banco de vulnerabilidades, porque o motor de análise associa o módulo ao código do Moby/daemon.

Este repositório usa o pacote **apenas como cliente HTTP** da API do Docker Engine (imagens, contentores, redes). Os vetores descritos nos avisos referem-se a componentes do **daemon** (AuthZ, plugins), não ao binário `godeployd`. Mesmo assim, execute `govulncheck ./...` em cada release e mantenha o Engine Docker atualizado no host.

Mitigações aplicadas no `godeployd` e serviços relacionados:

- `/webhook`: **limite de body** (`http.MaxBytesReader`), **rate limit** por IP (`GODEPLOY_WEBHOOK_RPS` / `GODEPLOY_WEBHOOK_BURST`), respostas de erro **sem vazamento** de detalhes internos em 500.
- **Headers de segurança** HTTP (nosniff, frame deny, referrer-policy, permissions-policy, `Cache-Control: no-store` no `godeployd`).
- **WebSocket** `/api/ws/logs`: `CheckOrigin` restritivo (mesmo host + lista opcional `GODEPLOY_WS_ALLOWED_ORIGINS`); mensagens de erro de stream sem detalhes do daemon.
- **Proxy** (`internal/proxy`): timeouts no `http.Server` e `Transport` para upstream.
- **Pipeline**: cliente HTTP do healthcheck com `Transport` com timeouts explícitos.
- Logging estruturado com **`log/slog`** no daemon e na pipeline.

Se você acredita que existe um vetor explorável neste repositório, por favor reporte de forma responsável conforme as instruções acima.
