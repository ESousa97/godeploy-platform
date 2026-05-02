# godeploy-platform

> Mini PaaS em Go: deteta o tipo de app, constrói imagem Docker e faz deploy com proxy por domínio e webhooks Git.

[![CI](https://github.com/esousa97/godeploy-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/esousa97/godeploy-platform/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/esousa97/godeploy-platform)](https://goreportcard.com/report/github.com/esousa97/godeploy-platform)
[![Go Reference](https://pkg.go.dev/badge/github.com/esousa97/godeploy-platform.svg)](https://pkg.go.dev/github.com/esousa97/godeploy-platform)
[![CodeFactor](https://www.codefactor.io/repository/github/esousa97/godeploy-platform/badge)](https://www.codefactor.io/repository/github/esousa97/godeploy-platform)
[![License](https://img.shields.io/github/license/esousa97/godeploy-platform)](https://github.com/esousa97/godeploy-platform/blob/main/LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/esousa97/godeploy-platform)](https://github.com/esousa97/godeploy-platform/blob/main/go.mod)
[![Last Commit](https://img.shields.io/github/last-commit/esousa97/godeploy-platform)](https://github.com/esousa97/godeploy-platform/commits/main)

---

O **godeploy-platform** é um daemon HTTP (`godeployd`) que recebe webhooks de push (GitHub ou GitLab), clona o repositório, deteta o runtime (Go, Node, Python, estático ou Dockerfile próprio), gera ou reutiliza um Dockerfile, faz build na API do Docker Engine, sobe um contentor com limites de CPU/RAM e atualiza rotas numa base SQLite para um reverse proxy opcional. Inclui ainda uma TUI para inspeção e um cliente WebSocket de logs. O módulo Go chama-se `godeploy-platform` (raiz do repositório); os badges acima assumem o repositório canónico `github.com/esousa97/godeploy-platform`.

## Demonstração

Fluxo típico após configurar variáveis de ambiente (ver `.env.example`):

```bash
make build
./bin/godeployd
```

Noutro terminal, health check:

```bash
curl -sS http://127.0.0.1:8081/healthz
```

Resposta esperada: corpo `ok` (ou equivalente configurado pelo servidor).

Cliente de logs (com `GODEPLOY_LOG_WS_URL` apontando para o daemon):

```bash
./bin/godeploy-logtail <CONTAINER_ID>
```

## Stack tecnológico

| Componente | Uso |
|------------|-----|
| Go 1.25+ | Linguagem e toolchain |
| Docker Engine API (`moby/client`) | Build de imagens, redes, contentores |
| `database/sql` + `modernc.org/sqlite` | Rotas do proxy e estado local |
| `gorilla/websocket` | Streaming de logs |
| Charm (`bubbletea`, `bubbles`, `lipgloss`) | TUI operacional |

## Pré-requisitos

- Go compatível com `go.mod` (1.25.0 ou superior na prática).
- Docker Engine acessível (socket ou host remoto via env padrão do cliente Docker).
- `git` no `PATH` para clones da pipeline.
- Opcional: `golangci-lint`, `govulncheck`, `gosec` para validação local alinhada ao Makefile e à CI.

## Instalação e uso

### A partir do source (recomendado)

```bash
git clone https://github.com/esousa97/godeploy-platform.git
cd godeploy-platform
cp .env.example .env
```

Defina as variáveis `GODEPLOY_*` no ambiente (o Go não carrega `.env` automaticamente). Em bash:

```bash
set -a && source .env && set +a
make build
make run
```

Ou, sem Makefile:

```bash
go build -o bin/godeployd ./cmd/godeployd
./bin/godeployd
```

### Como binário com `go install`

Quando o módulo estiver publicado sob um caminho `github.com/...` alinhado ao `go.mod`, pode usar:

```bash
go install github.com/esousa97/godeploy-platform/cmd/godeployd@latest
```

Enquanto o `go.mod` declarar apenas `module godeploy-platform`, prefira `make build` a partir do clone.

### Docker Compose

```bash
docker compose -f deployments/docker-compose.yml up --build
```

## Makefile targets

| Target | Descrição |
|--------|-----------|
| `help` | Lista targets documentados |
| `fmt` | `gofmt -w -s .` e `goimports` (via `go run golang.org/x/tools/cmd/goimports@latest -w .`) |
| `vet` | `go vet ./...` |
| `test` | `go test ./...` |
| `test-short` | Testes com `-short` (pula integrações que exigem Docker) |
| `test-race` | Testes com detector de corrida |
| `test-cover` | Cobertura e `coverage.html` |
| `test-cover-check` | `-short` + piso de cobertura (`COVER_MIN`, predefinido 29%) |
| `tidy` | `go mod tidy` |
| `build` | Compila os três binários em `bin/` |
| `build-daemon` | Apenas `godeployd` |
| `build-tui` | Apenas `godeploy-tui` |
| `build-logtail` | Apenas `godeploy-logtail` |
| `run` | `go run ./cmd/godeployd` |
| `lint` | `golangci-lint run ./...` |
| `vulncheck` | `govulncheck ./...` |
| `sec` | `gosec ./...` (requer binário instalado) |
| `validate` | `fmt`, `vet`, `lint`, `test`, `build` |
| `validate-full` | `fmt`, `vet`, `lint`, `test-short`, `test-cover-check`, `build` |
| `install-hooks` | `git config core.hooksPath .githooks` |
| `all` | `fmt`, `vet`, `test`, `build` |
| `generate` | `go generate ./...` |
| `docker-build` | Imagem `godeployd:local` via `deployments/Dockerfile` |
| `clean` | Remove `bin/`, `coverage.txt`, `coverage.html` e caches de teste Go |

## Arquitetura

- `cmd/godeployd` — servidor HTTP, wiring da `internal/pipeline`, webhooks e observabilidade.
- `cmd/godeploy-tui` — interface terminal sobre Docker e SQLite.
- `cmd/godeploy-logtail` — cliente WebSocket de logs.
- `internal/pipeline` — orquestração clone → build → deploy → health → rota.
- `internal/builder`, `internal/detector`, `internal/scheduler` — build, deteção e contentores.
- `internal/proxy` — store SQLite e reverse proxy por `Host`.
- `internal/webhook`, `internal/middleware`, `internal/observability` — entrada e transversal.

Diagrama lógico e decisões: [docs/architecture.md](docs/architecture.md). Deploy e TLS: [docs/deployment.md](docs/deployment.md).

## Documentação

| Documento | Conteúdo |
|-----------|-----------|
| [docs/setup.md](docs/setup.md) | Ambiente, CI local, Windows, proxy atrás de TLS |
| [docs/api.md](docs/api.md) | Contrato HTTP do `godeployd` |
| [docs/architecture.md](docs/architecture.md) | Fluxo de deploy e componentes |
| [docs/deployment.md](docs/deployment.md) | Notas de produção e checklist |

## API reference

Resumo HTTP do `godeployd`:

| Método | Rota | Descrição |
|--------|------|-----------|
| GET | `/healthz` | Liveness |
| POST | `/webhook` | Push GitHub/GitLab (corpo JSON, cabeçalhos do provider) |
| GET | `/api/stats` | JSON com estatísticas de contentores |
| GET | `/api/ws/logs?container=` | Upgrade WebSocket para logs |

Detalhe e exemplos: [docs/api.md](docs/api.md). Documentação de pacotes `internal/` via `go doc` no clone local (pacotes não são API pública estável).

## Configuração

| Variável | Tipo | Default | Descrição |
|----------|------|---------|-----------|
| `GODEPLOY_ADDR` | string | `:8081` | Endereço de escuta HTTP |
| `GODEPLOY_DB` | path | `godeploy.db` | Ficheiro SQLite |
| `GODEPLOY_NETWORK` | string | `godeploy` | Nome da rede Docker dos apps |
| `GODEPLOY_IMAGE_PREFIX` | string | `godeploy` | Prefixo de nome de imagem |
| `GODEPLOY_HEALTH_PATH` | string | `/` | Caminho HTTP do healthcheck na pipeline |
| `GODEPLOY_WEBHOOK_SECRET` | string | vazio | Secret HMAC GitHub / token GitLab (vazio desativa verificação) |
| `GODEPLOY_WEBHOOK_RPS` | float | `5` | Rate limit médio por IP no POST `/webhook` |
| `GODEPLOY_WEBHOOK_BURST` | int | `30` | Rajada permitida no rate limit |
| `GODEPLOY_WS_ALLOWED_ORIGINS` | lista CSV | vazio | Origens extra permitidas no WebSocket de logs |
| `GODEPLOY_LOG_WS_URL` | URL WS | `ws://127.0.0.1:8081/api/ws/logs` | Base URL para `godeploy-logtail` |

## Roadmap

- [ ] CLI unificada (`cmd/godeploy`) com subcomandos `detect`, `build`, `deploy`
- [ ] Healthcheck e variáveis de ambiente por app no deploy
- [ ] Rollout com readiness prolongado e rollback explícito
- [ ] Push de imagens para GHCR e deploy por digest

## Contribuindo

Veja [CONTRIBUTING.md](CONTRIBUTING.md) e [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Licença

[MIT](LICENSE).

## Autor

**Enoque Sousa** — [Portfolio](https://enoquesousa.vercel.app) · [GitHub](https://github.com/esousa97)
