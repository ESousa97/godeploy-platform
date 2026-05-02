# Architecture (high-level)

## Deploy flow (webhook)

```mermaid
sequenceDiagram
  participant GH as GitHub/GitLab
  participant D as godeployd
  participant P as pipeline.Runner
  participant DK as Docker Engine
  participant DB as SQLite

  GH->>D: POST /webhook (push)
  D->>P: Run(ctx, RunRequest)
  P->>P: git clone (temp dir)
  P->>P: detector.Detect
  P->>DK: ImageBuild (context tar)
  P->>DK: Deploy new container (KeepOld)
  P->>P: HTTP health on published host:port
  P->>DB: UpsertRoute(domain, target)
  P->>DK: stop/remove old container
```

## Components

- **godeployd** wires security middleware, webhook rate limiting, event parsing, and a `pipeline.Runner` with a shared Docker client and `sql.DB`.
- **pipeline** orchestrates: isolation in a temp directory, build with log streaming to `slog`, rollout with health check before switching routes.
- **proxy** keeps an in-memory `Host` → `ip:port` map, reloaded by version in `proxy_meta` and optional notification after deploy.
- **scheduler** ensures the bridge network labelled `godeploy.managed`, publishes ports, and applies resource limits.

## HTTP hardening and data (daemon)

- **`godeployd`**: `ReadHeaderTimeout` and `ReadTimeout` on `http.Server` for request reads; `WriteTimeout` omitted for long handlers (webhook) and WebSocket upgrade; `MaxBytesReader` on the webhook body.
- **SQLite**: conservative pooling via `internal/platform/sqlpool` (`MaxOpenConns(1)`, `ConnMaxLifetime`) in daemon and TUI.
- **Middleware**: security headers including `Cache-Control: no-store` on the main mux responses.

## Decisions

- **Embedded SQLite** (no server process) for simplicity in a learning setup; not a distributed control plane.
- **internal/** makes it explicit there is no stable public API between releases.
- **WebSocket**: restrictive `CheckOrigin`; same origin and optional list via env.
- **TLS**: termination outside the Go process (see [deployment.md](deployment.md)).
