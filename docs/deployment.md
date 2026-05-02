# Deploy em produção (notas)

## Daemon (`godeployd`)

- O processo escuta em `GODEPLOY_ADDR` (por defeito `:8081`) **sem TLS**. Coloque um reverse proxy (Caddy, nginx, traefik ou load balancer cloud) na frente para HTTPS e HSTS.
- O ficheiro `deployments/docker-compose.yml` exemplifica socket Docker montado e volume para `GODEPLOY_DB`. Ajuste portas, rede e **nunca** exponha `GODEPLOY_WEBHOOK_SECRET` em logs ou imagens públicas.
- Defina `GODEPLOY_WEBHOOK_SECRET` em produção e mantenha rate limits (`GODEPLOY_WEBHOOK_RPS` / `GODEPLOY_WEBHOOK_BURST`) adequados ao tráfego esperado.

## Reverse proxy de apps (`internal/proxy`)

- Binário separado (integração no teu arranque): escuta tipicamente em `:80` ou `:8080` e lê rotas do **mesmo** SQLite que o daemon atualiza após deploy.
- Após cada `UpsertRoute`, chame `NotifyReload` no `*proxy.Proxy` se quiseres propagar a rota sem esperar o poll (a pipeline do repositório segue este padrão quando ligada ao proxy).

## Checklist rápido

1. Docker Engine atualizado no host (mitigação operacional para avisos `govulncheck` no cliente Go).
2. Backup periódico de `GODEPLOY_DB`.
3. Firewall: apenas proxies e redes de confiança devem alcançar `godeployd` e o socket Docker.

Para variáveis e fluxo local, ver [setup.md](setup.md) e [architecture.md](architecture.md).
