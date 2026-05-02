# Production deployment (notes)

## Daemon (`godeployd`)

- The process listens on `GODEPLOY_ADDR` (default `:8081`) **without TLS**. Put a reverse proxy (Caddy, nginx, Traefik, or a cloud load balancer) in front for HTTPS and HSTS.
- `deployments/docker-compose.yml` shows a mounted Docker socket and volume for `GODEPLOY_DB`. Tune ports, networks, and **never** expose `GODEPLOY_WEBHOOK_SECRET` in logs or public images.
- Set `GODEPLOY_WEBHOOK_SECRET` in production and keep webhook rate limits (`GODEPLOY_WEBHOOK_RPS` / `GODEPLOY_WEBHOOK_BURST`) appropriate for expected traffic.

## App reverse proxy (`internal/proxy`)

- Separate binary (integrate in your startup): typically listens on `:80` or `:8080` and reads routes from the **same** SQLite the daemon updates after deploy.
- After each `UpsertRoute`, call `NotifyReload` on `*proxy.Proxy` if you want the route propagated without waiting for the poll (this repository’s pipeline follows that pattern when wired to the proxy).

## Quick checklist

1. Up-to-date Docker Engine on the host (operational mitigation for `govulncheck` warnings on the Go client).
2. Periodic backup of `GODEPLOY_DB`.
3. Firewall: only trusted proxies and networks should reach `godeployd` and the Docker socket.

For variables and local flow, see [setup.md](setup.md) and [architecture.md](architecture.md).
