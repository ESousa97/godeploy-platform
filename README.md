<div align="center">
<h1>godeploy-platform</h1>

<p>Mini PaaS in Go: detects app type, builds a Docker image, and deploys with domain-based proxy routing and Git webhooks.</p>

  <img src="assets/github-go.png" alt="gochangelog-gen Banner" width="600px">

  <br>


[![CI](https://github.com/esousa97/godeploy-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/esousa97/godeploy-platform/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/esousa97/godeploy-platform)](https://goreportcard.com/report/github.com/esousa97/godeploy-platform)
[![Go Reference](https://pkg.go.dev/badge/github.com/esousa97/godeploy-platform.svg)](https://pkg.go.dev/github.com/esousa97/godeploy-platform)
[![CodeFactor](https://www.codefactor.io/repository/github/esousa97/godeploy-platform/badge)](https://www.codefactor.io/repository/github/esousa97/godeploy-platform)
[![License](https://img.shields.io/github/license/esousa97/godeploy-platform)](https://github.com/esousa97/godeploy-platform/blob/main/LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/esousa97/godeploy-platform)](https://github.com/esousa97/godeploy-platform/blob/main/go.mod)
[![Last Commit](https://img.shields.io/github/last-commit/esousa97/godeploy-platform)](https://github.com/esousa97/godeploy-platform/commits/main)

</div>

---

**godeploy-platform** is an HTTP daemon (`godeployd`) that receives push webhooks (GitHub or GitLab), clones the repository, detects the runtime (Go, Node, Python, static, or a supplied Dockerfile), generates or reuses a Dockerfile, builds via the Docker Engine API, runs a container with CPU/RAM limits, and updates routes in SQLite for an optional reverse proxy. It also includes a TUI for inspection and a WebSocket log client. The Go module is named `godeploy-platform` (repository root); the badges above assume the canonical repo `github.com/esousa97/godeploy-platform`.

## Demo (60-second smoke test)

Typical flow after configuring environment variables (`.env.example`). Build, run, and two liveness checks (HTTP + webhook ping):

**Linux / macOS (bash)**

```bash
make build
unset GODEPLOY_WEBHOOK_SECRET
GODEPLOY_ADDR=:8081 GODEPLOY_DB=godeploy.db GODEPLOY_NETWORK=godeploy ./bin/godeployd &
sleep 1
curl -sS http://127.0.0.1:8081/healthz                                # → ok
curl -sS -X POST http://127.0.0.1:8081/webhook \
  -H "X-GitHub-Event: ping" -H "Content-Type: application/json" -d '{}'  # → pong
```

**Windows (PowerShell)**

```powershell
go build -o .\bin\godeployd.exe .\cmd\godeployd
$env:GODEPLOY_ADDR=":8081"; $env:GODEPLOY_DB="godeploy.db"; $env:GODEPLOY_NETWORK="godeploy"
Remove-Item Env:\GODEPLOY_WEBHOOK_SECRET -ErrorAction SilentlyContinue
Start-Process -NoNewWindow -FilePath .\bin\godeployd.exe
Start-Sleep -Seconds 1
(Invoke-WebRequest http://127.0.0.1:8081/healthz -UseBasicParsing).Content   # → ok
(Invoke-WebRequest -Method POST -Uri http://127.0.0.1:8081/webhook `
   -Headers @{ "X-GitHub-Event" = "ping" } -ContentType application/json `
   -Body "{}" -UseBasicParsing).Content                                       # → pong
```

Real-time log client (with `GODEPLOY_LOG_WS_URL` pointing at the daemon):

```bash
./bin/godeploy-logtail <CONTAINER_ID>
```

For an end-to-end deploy (clone → build → run → health check → route), see the [Tutorial](#tutorial-from-zero-to-your-first-real-deploy) below.

## Tech stack

| Component | Role |
|-----------|------|
| Go 1.25+ | Language and toolchain |
| Docker Engine API (`moby/client`) | Image builds, networks, containers |
| `database/sql` + `modernc.org/sqlite` | Proxy routes and local state |
| `gorilla/websocket` | Log streaming |
| Charm (`bubbletea`, `bubbles`, `lipgloss`) | Operational TUI |

## Prerequisites

- Go compatible with `go.mod` (1.25.0 or newer in practice).
- Docker Engine reachable (socket or remote host via the Docker client’s default env).
- `git` on `PATH` for pipeline clones.
- Optional: `golangci-lint`, `govulncheck`, `gosec` for local validation aligned with the Makefile and CI.

## Installation and usage

### From source (recommended)

```bash
git clone https://github.com/esousa97/godeploy-platform.git
cd godeploy-platform
cp .env.example .env
```

Set `GODEPLOY_*` in the environment (Go does not auto-load `.env`). In bash:

```bash
set -a && source .env && set +a
make build
make run
```

Or without the Makefile:

```bash
go build -o bin/godeployd ./cmd/godeployd
./bin/godeployd
```

### As a binary with `go install`

When the module is published under a `github.com/...` path aligned with `go.mod`, you can use:

```bash
go install github.com/esousa97/godeploy-platform/cmd/godeployd@latest
```

While `go.mod` declares only `module godeploy-platform`, prefer `make build` from a clone.

### Docker Compose

```bash
docker compose -f deployments/docker-compose.yml up --build
```

## Tutorial: from zero to your first real deploy

This tutorial runs the full pipeline (`webhook → clone → build → run → health check → route`) without configuring a webhook on GitHub/GitLab. It is the fastest way to validate changes in `internal/pipeline` before production webhooks. Tested on Windows + Docker Desktop and Linux + Docker Engine.

### Preconditions (quick checks)

```bash
docker info >/dev/null && echo "docker ok"
git --version
test -x bin/godeployd && echo "binary ready" || make build-daemon
```

```powershell
docker info | Out-Null; "docker ok"
git --version
if (Test-Path .\bin\godeployd.exe) { "binary ready" } else { go build -o .\bin\godeployd.exe .\cmd\godeployd }
```

Start the daemon **in another terminal** and leave it running until the final step:

```bash
unset GODEPLOY_WEBHOOK_SECRET
GODEPLOY_ADDR=:8081 GODEPLOY_DB=godeploy.db GODEPLOY_NETWORK=godeploy ./bin/godeployd
```

```powershell
$env:GODEPLOY_ADDR=":8081"; $env:GODEPLOY_DB="godeploy.db"; $env:GODEPLOY_NETWORK="godeploy"
Remove-Item Env:\GODEPLOY_WEBHOOK_SECRET -ErrorAction SilentlyContinue
.\bin\godeployd.exe
```

Confirm `level=INFO msg="godeployd listening" addr=:8081` in the log and that `GET /healthz` returns `200 ok`.

### Step 1 — create a simulation app (local git repo)

Publish a minimal nginx listening on **8080** (default port from `RuntimeDockerfile` in `internal/pipeline.defaultPortForRuntime`) with its own `/healthz` endpoint.

**Bash**

```bash
SIM=$(mktemp -d -t godeploy-sim-XXXX)
cat > "$SIM/Dockerfile" <<'DOCKERFILE'
FROM nginx:alpine
RUN printf '%s\n' \
  'server {' \
  '    listen 8080 default_server;' \
  '    location = /healthz { return 200 "ok"; add_header Content-Type text/plain; }' \
  '    location / { root /usr/share/nginx/html; index index.html; }' \
  '}' > /etc/nginx/conf.d/default.conf \
 && printf '<h1>godeploy sim ok</h1>' > /usr/share/nginx/html/index.html
EXPOSE 8080
DOCKERFILE
( cd "$SIM" \
  && git init -q && git checkout -b main 2>/dev/null \
  && git -c user.email=sim@local -c user.name=sim add . \
  && git -c user.email=sim@local -c user.name=sim commit -q -m "sim app v1" )
CLONE_URL="file://$SIM"
echo "clone_url = $CLONE_URL"
```

**PowerShell**

```powershell
$sim = Join-Path $env:TEMP ("godeploy-sim-" + (Get-Random))
New-Item -ItemType Directory -Path $sim | Out-Null
@"
FROM nginx:alpine
RUN printf '%s\n' 'server {' '    listen 8080 default_server;' '    location = /healthz { return 200 "ok"; add_header Content-Type text/plain; }' '    location / { root /usr/share/nginx/html; index index.html; }' '}' > /etc/nginx/conf.d/default.conf && printf '<h1>godeploy sim ok</h1>' > /usr/share/nginx/html/index.html
EXPOSE 8080
"@ | Set-Content -Encoding ascii (Join-Path $sim 'Dockerfile')
Push-Location $sim
git init -q; git checkout -b main 2>$null
git -c user.email=sim@local -c user.name=sim add .
git -c user.email=sim@local -c user.name=sim commit -q -m "sim app v1"
Pop-Location
$cloneURL = "file:///" + ($sim -replace '\\','/')
"clone_url = $cloneURL"
```

> **Note:** the pipeline runs `git clone --depth 1 --branch <ref>`. Working-tree-only changes are ignored — godeployd logs `level=WARN msg="local repository working tree has uncommitted changes; build will use HEAD only"`.

### Step 2 — trigger the push webhook

`POST /webhook` accepts two optional query parameters:

| Query | Default | Effect |
|---|---|---|
| `domain` | `<app>.local` (`normalizeApp(repository.name)`) | Key stored in `proxy_routes` for the reverse proxy |
| `health_path` | `GODEPLOY_HEALTH_PATH` or `/` | HTTP path used to validate the new container before switching the route |

**Bash**

```bash
BODY=$(jq -nc --arg url "$CLONE_URL" \
  '{ref:"refs/heads/main", after:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", repository:{name:"sim-app", clone_url:$url}}')

curl -sS -X POST \
  "http://127.0.0.1:8081/webhook?domain=sim.local&health_path=/healthz" \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: push" \
  -d "$BODY" | jq
```

**PowerShell**

```powershell
$body = @{
  ref = "refs/heads/main"
  after = ("a"*40)
  repository = @{ name = "sim-app"; clone_url = $cloneURL }
} | ConvertTo-Json -Compress

$resp = Invoke-WebRequest `
  -Uri "http://127.0.0.1:8081/webhook?domain=sim.local&health_path=/healthz" `
  -Method POST -ContentType "application/json" `
  -Headers @{ "X-GitHub-Event" = "push" } `
  -Body $body -UseBasicParsing -TimeoutSec 300
$resp.Content | ConvertFrom-Json | Format-List
```

Expected response (`200 OK`, `application/json`):

```json
{
  "provider": "github",
  "app": "sim-app",
  "runtime": "dockerfile",
  "image_tag": "godeploy/sim-app:20260502-224013-aaaaaaa",
  "new_container_id": "d6199d0367...",
  "old_container_id": "",
  "routed_target": "127.0.0.1:8080"
}
```

### Step 3 — verify container and traffic

```bash
docker ps --filter "label=godeploy.app.name=sim-app" \
  --format "table {{.Names}}\t{{.Image}}\t{{.Ports}}\t{{.Status}}"
curl -sS http://127.0.0.1:8080/healthz   # → ok
curl -sS http://127.0.0.1:8080/          # → <h1>godeploy sim ok</h1>
curl -sS http://127.0.0.1:8081/api/stats | jq '.containers[] | {name,state,cpu_percent,mem_percent}'
```

```powershell
docker ps --filter "label=godeploy.app.name=sim-app" `
  --format "table {{.Names}}`t{{.Image}}`t{{.Ports}}`t{{.Status}}"
(Invoke-WebRequest http://127.0.0.1:8080/healthz -UseBasicParsing).Content
(Invoke-WebRequest http://127.0.0.1:8080/ -UseBasicParsing).Content
(Invoke-WebRequest http://127.0.0.1:8081/api/stats -UseBasicParsing).Content | ConvertFrom-Json
```

`/api/stats` reports `mem_limit_bytes=268435456` (256 MiB scheduler default) and in-use CPU/RAM.

### Step 4 — re-deploy (blue-green rollout)

Edit, commit, and send the same POST again. Because an old version exists, the scheduler:

1. creates the new container on a **dynamic host port** (avoids conflict with v1);
2. health-checks the `health_path` you passed;
3. updates the route in SQLite;
4. only then stops and removes the old container.

```bash
( cd "$SIM" \
  && cat > Dockerfile <<'DOCKERFILE'
FROM nginx:alpine
RUN printf '%s\n' \
  'server {' \
  '    listen 8080 default_server;' \
  '    location = /healthz { return 200 "ok-v2"; add_header Content-Type text/plain; }' \
  '    location / { root /usr/share/nginx/html; index index.html; }' \
  '}' > /etc/nginx/conf.d/default.conf \
 && printf '<h1>godeploy sim v2 (blue-green)</h1>' > /usr/share/nginx/html/index.html
EXPOSE 8080
DOCKERFILE
  git -c user.email=sim@local -c user.name=sim commit -aq -m "v2" )
# repeat the POST from step 2; routed_target will use a different port
```

```powershell
Push-Location $sim
(Get-Content Dockerfile) -replace 'sim ok','sim v2 (blue-green)' | Set-Content -Encoding ascii Dockerfile
git -c user.email=sim@local -c user.name=sim commit -aq -m "v2"
Pop-Location
# repeat the POST from step 2
```

The new response has `routed_target` on a dynamic port (e.g. `127.0.0.1:61695`) and `old_container_id` set to the previous ID. Confirm the old one is gone:

```bash
docker ps -a --filter "label=godeploy.app.name=sim-app"
```

### Step 5 — simulate failure and read container logs

Create an app that exits with an error and see stdout/stderr tail embedded in the pipeline error (from `internal/scheduler.fetchContainerLogsTail`):

```bash
BAD=$(mktemp -d -t godeploy-bad-XXXX)
cat > "$BAD/Dockerfile" <<'DOCKERFILE'
FROM busybox:latest
CMD ["sh","-c","echo bang; echo 'stderr error' >&2; exit 42"]
DOCKERFILE
( cd "$BAD" && git init -q && git checkout -b main 2>/dev/null \
  && git -c user.email=sim@local -c user.name=sim add . \
  && git -c user.email=sim@local -c user.name=sim commit -q -m "broken" )
BODY=$(jq -nc --arg url "file://$BAD" \
  '{ref:"refs/heads/main", after:"cccccccccccccccccccccccccccccccccccccccc", repository:{name:"broken-app", clone_url:$url}}')
curl -sS -i -X POST -H "X-GitHub-Event: push" -H "Content-Type: application/json" \
  -d "$BODY" "http://127.0.0.1:8081/webhook?domain=broken.local"
```

The HTTP response is `500 deploy failed`, but the **`godeployd` log shows the real cause**:

```
level=ERROR msg="pipeline failed" app=broken-app provider=github
err="new container \"broken-app-...\" did not reach running:
     container exited early with code 42; logs:
     stderr error
     bang"
```

### Step 6 — reverse proxy by domain

`internal/proxy` is a library, not a binary. The quickest local test is a short wrapper sharing the same `GODEPLOY_DB` godeployd updated:

```bash
mkdir -p cmd/godeploy-proxy-dev
cat > cmd/godeploy-proxy-dev/main.go <<'GO'
package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
	"godeploy-platform/internal/proxy"
)

func main() {
	db, err := sql.Open("sqlite", os.Getenv("GODEPLOY_DB"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	p, err := proxy.New(proxy.Config{Addr: ":8090", DB: db, PollInterval: time.Second})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Println("proxy on :8090")
	if err := p.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
GO
GODEPLOY_DB=$(pwd)/godeploy.db go run ./cmd/godeploy-proxy-dev
```

In another terminal, hit it with the **Host header** (the proxy routes by domain):

```bash
curl -sS -H "Host: sim.local" http://127.0.0.1:8090/healthz   # → ok
curl -sS -H "Host: sim.local" http://127.0.0.1:8090/          # → <h1>godeploy sim ...
```

For the domain in a browser, add `127.0.0.1 sim.local` to `/etc/hosts` (Linux/macOS) or `C:\Windows\System32\drivers\etc\hosts` (Windows) and open `http://sim.local:8090/`.

### Step 7 — cleanup

```bash
docker ps -a --filter "label=godeploy.managed=true" -q | xargs -r docker rm -f
docker network rm godeploy 2>/dev/null
docker images "godeploy/sim-app" -q | xargs -r docker rmi -f
docker images "godeploy/broken-app" -q | xargs -r docker rmi -f
rm -rf "$SIM" "$BAD" godeploy.db
# Stop godeployd with Ctrl+C in its terminal.
```

```powershell
docker ps -a --filter "label=godeploy.managed=true" --format "{{.ID}}" | ForEach-Object { docker rm -f $_ } | Out-Null
docker network rm godeploy 2>$null | Out-Null
docker images --filter "reference=godeploy/sim-app" --format "{{.ID}}" | ForEach-Object { docker rmi -f $_ } | Out-Null
docker images --filter "reference=godeploy/broken-app" --format "{{.ID}}" | ForEach-Object { docker rmi -f $_ } | Out-Null
Get-ChildItem $env:TEMP -Directory | Where-Object { $_.Name -like "godeploy-sim-*" -or $_.Name -like "godeploy-bad-*" } | Remove-Item -Recurse -Force
Remove-Item godeploy.db -ErrorAction SilentlyContinue
Get-Process -Name godeployd -ErrorAction SilentlyContinue | Stop-Process -Force
```

## Makefile targets

| Target | Description |
|--------|-------------|
| `help` | List documented targets |
| `fmt` | `gofmt -w -s .` and `goimports` (via `go run golang.org/x/tools/cmd/goimports@latest -w .`) |
| `vet` | `go vet ./...` |
| `test` | `go test ./...` |
| `test-short` | Tests with `-short` (skips integrations that need Docker) |
| `test-race` | Tests with the race detector |
| `test-cover` | Coverage and `coverage.html` |
| `test-cover-check` | `-short` plus coverage floor (`COVER_MIN`, default 29%) |
| `tidy` | `go mod tidy` |
| `build` | Build all three binaries into `bin/` |
| `build-daemon` | `godeployd` only |
| `build-tui` | `godeploy-tui` only |
| `build-logtail` | `godeploy-logtail` only |
| `run` | `go run ./cmd/godeployd` |
| `lint` | `golangci-lint run ./...` |
| `vulncheck` | `govulncheck ./...` |
| `sec` | `gosec ./...` (requires the binary installed) |
| `validate` | `fmt`, `vet`, `lint`, `test`, `build` |
| `validate-full` | `fmt`, `vet`, `lint`, `test-short`, `test-cover-check`, `build` |
| `install-hooks` | `git config core.hooksPath .githooks` |
| `all` | `fmt`, `vet`, `test`, `build` |
| `generate` | `go generate ./...` |
| `docker-build` | Image `godeployd:local` via `deployments/Dockerfile` |
| `clean` | Remove `bin/`, `coverage.txt`, `coverage.html`, and Go test caches |

## Architecture

- `cmd/godeployd` — HTTP server, `internal/pipeline` wiring, webhooks, observability.
- `cmd/godeploy-tui` — terminal UI over Docker and SQLite.
- `cmd/godeploy-logtail` — WebSocket log client.
- `internal/pipeline` — orchestration: clone → build → deploy → health → route.
- `internal/builder`, `internal/detector`, `internal/scheduler` — build, detection, containers.
- `internal/proxy` — SQLite route store and Host-based reverse proxy.
- `internal/webhook`, `internal/middleware`, `internal/observability` — ingress and cross-cutting concerns.

Logical diagram and decisions: [docs/architecture.md](docs/architecture.md). Deploy and TLS: [docs/deployment.md](docs/deployment.md).

## Documentation

| Document | Contents |
|----------|----------|
| [docs/setup.md](docs/setup.md) | Environment, local CI, Windows, TLS fronting |
| [docs/api.md](docs/api.md) | `godeployd` HTTP contract |
| [docs/architecture.md](docs/architecture.md) | Deploy flow and components |
| [docs/deployment.md](docs/deployment.md) | Production notes and checklist |

## API reference

HTTP summary for `godeployd`:

| Method | Route | Description |
|--------|-------|-------------|
| GET | `/healthz` | Liveness (`200 ok`) |
| POST | `/webhook` | GitHub/GitLab push (JSON body, provider headers). Optional query: `domain` (SQLite route key, default `<app>.local`) and `health_path` (HTTP path for the new container’s health check, default `GODEPLOY_HEALTH_PATH` or `/`). Success: `200` JSON with `routed_target`. Failure: `4xx`/`5xx`; details only in logs. |
| GET | `/api/stats` | JSON container statistics (CPU/RAM per container) |
| GET | `/api/ws/logs?container=` | WebSocket upgrade for container logs (same origin or `GODEPLOY_WS_ALLOWED_ORIGINS`) |

Details and examples: [docs/api.md](docs/api.md). Package docs for `internal/` via `go doc` in a local clone (not a stable public API).

## Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `GODEPLOY_ADDR` | string | `:8081` | HTTP listen address |
| `GODEPLOY_DB` | path | `godeploy.db` | SQLite file |
| `GODEPLOY_NETWORK` | string | `godeploy` | Docker bridge network for apps |
| `GODEPLOY_IMAGE_PREFIX` | string | `godeploy` | Image name prefix |
| `GODEPLOY_HEALTH_PATH` | string | `/` | HTTP health-check path in the pipeline |
| `GODEPLOY_WEBHOOK_SECRET` | string | empty | GitHub HMAC secret / GitLab token (empty disables verification) |
| `GODEPLOY_WEBHOOK_RPS` | float | `5` | Average POST `/webhook` rate limit per IP |
| `GODEPLOY_WEBHOOK_BURST` | int | `30` | Burst size for the webhook rate limiter |
| `GODEPLOY_WS_ALLOWED_ORIGINS` | CSV list | empty | Extra allowed origins for the log WebSocket |
| `GODEPLOY_LOG_WS_URL` | WS URL | `ws://127.0.0.1:8081/api/ws/logs` | Base URL for `godeploy-logtail` |
| `GODEPLOY_BIND_DOCKER_SOCK` | bool (`1`/`true`/`yes`/`on`) | `false` | When enabled, the **scheduler** bind-mounts `/var/run/docker.sock` into every deployed container. Use only to deploy `godeployd` itself (self-host) or tools that must talk to the host Docker engine. **High security risk** — effectively root on the host. |

## Troubleshooting

Most pipeline failures show up as `level=ERROR msg="pipeline failed"` in `godeployd` structured logs (the `/webhook` HTTP response is intentionally generic `deploy failed`, without stack traces to the client).

| Symptom | Likely cause | What to do |
|---|---|---|
| `err="new container ... did not reach running: container exited early with code N; logs: ..."` | Process inside the container exited before `running` | stdout/stderr tail is **embedded in the error**. Reproduce with `docker run --rm <image_tag>`. |
| `level=WARN msg="local repository working tree has uncommitted changes; build will use HEAD only" dirty_files=N` | `clone_url=file://...` points at a dirty local repo | `git clone --depth 1 --branch <ref>` always copies the remote HEAD. `git commit` before firing the webhook, or accept those changes will not ship. |
| `err="scheduler: failed to list networks: Cannot connect to the Docker daemon at unix:///var/run/docker.sock..."` | godeployd runs in a container without the Docker socket | Bind-mount the socket in compose (`/var/run/docker.sock:/var/run/docker.sock`). If godeployd deploys itself, set `GODEPLOY_BIND_DOCKER_SOCK=true` in the **source** that triggers the build. |
| `err="port conflict (8080): port already published by container ..."` | **First** deploy of an app whose `InternalPort` (8080 default for `Dockerfile`/`Go`, 3000 Node, 8000 Python, 80 `Static`) is already bound on the host | Stop the conflicting container, or use a different internal port. From the second deploy onward the scheduler uses a dynamic port (blue-green). |
| `invalid GitHub signature` in the log + 4xx from the provider | `GODEPLOY_WEBHOOK_SECRET` set but `X-Hub-Signature-256` does not match | Confirm the provider secret matches the daemon and no proxy rewrites the body. |
| `unsupported GitHub event: <X>` or `unsupported GitLab event: <X>` | Webhook delivered something other than `push`/`ping` (GitHub) or `Push Hook` (GitLab) | Filter at the provider or handle 4xx in the consumer; other events are not processed. |
| `GET /api/stats` or `/api/ws/logs` return 404 | godeployd started without a Docker client (test-only wiring with `routeDeps.docker == nil`) | Production wiring is unconditional; check bootstrap logs. |
| `429 Too Many Requests` on `/webhook` | Exceeded per-IP token bucket (`GODEPLOY_WEBHOOK_RPS`/`GODEPLOY_WEBHOOK_BURST`) | Tune limits or throttle at the provider. |
| `bad gateway` from the proxy | Route exists in SQLite but `target` (`ip:port`) is down | Check `docker ps`, published port vs route (`/api/stats` helps). |

## Roadmap

End-to-end delivery in this repo: scheduler, detector/builder, proxy, pipeline/webhook, and observability.

- [x] **Stage 1 — Orchestration core (Docker SDK & engine)** — Container lifecycle, dedicated bridge network, CPU/RAM limits, blue-green style deploys, and handling for name/port conflicts (`internal/scheduler`).
- [x] **Stage 2 — Detection & build engine (zero config)** — Runtime detection from repo markers, embedded multi-stage templates when no Dockerfile exists, Docker image builds with unique tags, build log streaming (`internal/detector`, `internal/builder`).
- [x] **Stage 3 — Dynamic reverse proxy (ingress)** — Route by `Host`, SQLite `domain → target`, `httputil.ReverseProxy`, forwarding headers, in-memory hot reload (`internal/proxy`).
- [x] **Stage 4 — Automation & GitOps** — GitHub/GitLab webhook, temp clone, detector → builder → scheduler, health-based rollback (`internal/pipeline`, `internal/webhook`).
- [x] **Stage 5 — Dashboard & observability (TUI / API)** — Container stats, `/api/stats`, WebSocket log tail, optional Bubble Tea TUI (`internal/observability`, `cmd/godeploy-tui`).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## License

[MIT](LICENSE).

<div align="center">

## Author

**Enoque Sousa**

[![LinkedIn](https://img.shields.io/badge/LinkedIn-0077B5?style=flat&logo=linkedin&logoColor=white)](https://www.linkedin.com/in/enoque-sousa-bb89aa168/)
[![GitHub](https://img.shields.io/badge/GitHub-100000?style=flat&logo=github&logoColor=white)](https://github.com/esousa97)
[![Portfolio](https://img.shields.io/badge/Portfolio-FF5722?style=flat&logo=target&logoColor=white)](https://enoquesousa.vercel.app)

**[⬆ Back to Top](#godeploy-platform)**

Made with ❤️ by [Enoque Sousa](https://github.com/esousa97)

**Project Status:** Study project

</div>
