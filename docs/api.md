# HTTP API (`godeployd`)

Base URL: value of `GODEPLOY_ADDR` (default `http://127.0.0.1:8081`).

## `GET /healthz`

- **Response**: `200` with plain body `ok` (liveness).
- **Headers**: daemon responses go through security middleware (`Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, etc.).
- **Usage**: load balancers and Kubernetes probes.

## `POST /webhook`

- **Body**: raw JSON payload from GitHub or GitLab (same bytes used for signature verification).
- **GitHub headers**: `X-GitHub-Event`, optionally `X-Hub-Signature-256` when `GODEPLOY_WEBHOOK_SECRET` is set.
- **GitLab headers**: `X-Gitlab-Event`, optionally `X-Gitlab-Token` when a secret is configured.
- **Rate limit**: token bucket per remote IP (`GODEPLOY_WEBHOOK_RPS`, `GODEPLOY_WEBHOOK_BURST`).
- **Max body size**: 1 MiB in the current server.
- **Error responses**: generic messages on 4xx/5xx; details in structured logs.
- **Success (processed `push`)**: `200` with `Content-Type: application/json` and fields such as `provider`, `app`, `runtime`, `image_tag`, `new_container_id`, `old_container_id`, `routed_target` (see `handleWebhook` in `cmd/godeployd`).

Minimal example (GitHub ping or replace body with a real push):

```bash
curl -sS -X POST "http://127.0.0.1:8081/webhook" \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: ping" \
  -d '{}'
```

## `GET /api/stats`

- **Response**: `application/json` with a list of running containers and approximate CPU/memory metrics.
- **Internal timeout**: short window in the handler; collection failures return `500` without exposing stack traces to the client.

## `GET /api/ws/logs?container=<id|name>`

- **Upgrade**: WebSocket.
- **Query**: `container` is required (Docker ID or name).
- **Origin**: browser requests only with the same `Host` or origins listed in `GODEPLOY_WS_ALLOWED_ORIGINS`.
- **Messages**: JSON per line (`stream`, `line`) as implemented in `internal/observability`.

Reference client: `cmd/godeploy-logtail`.
