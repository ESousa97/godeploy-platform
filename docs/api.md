# API HTTP (`godeployd`)

Base URL: valor de `GODEPLOY_ADDR` (por defeito `http://127.0.0.1:8081`).

## `GET /healthz`

- **Resposta**: `200` com corpo textual `ok` (liveness).
- **Cabeçalhos**: as respostas do daemon passam pelo middleware de segurança (`Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, etc.).
- **Uso**: balanceadores e Kubernetes probes.

## `POST /webhook`

- **Corpo**: payload JSON bruto do GitHub ou GitLab (o mesmo corpo usado na verificação de assinatura).
- **Cabeçalhos GitHub**: `X-GitHub-Event`, opcionalmente `X-Hub-Signature-256` se `GODEPLOY_WEBHOOK_SECRET` estiver definido.
- **Cabeçalhos GitLab**: `X-Gitlab-Event`, opcionalmente `X-Gitlab-Token` se o secret estiver definido.
- **Rate limit**: token bucket por IP remoto (`GODEPLOY_WEBHOOK_RPS`, `GODEPLOY_WEBHOOK_BURST`).
- **Tamanho máximo do corpo**: 1 MiB no servidor atual.
- **Respostas de erro**: mensagens genéricas em 4xx/5xx; detalhes no log estruturado.
- **Sucesso (`push` processado)**: `200` com `Content-Type: application/json` e corpo com campos como `provider`, `app`, `runtime`, `image_tag`, `new_container_id`, `old_container_id`, `routed_target` (ver `handleWebhook` em `cmd/godeployd`).

Exemplo mínimo (GitHub ping ou push real substitui o corpo):

```bash
curl -sS -X POST "http://127.0.0.1:8081/webhook" \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: ping" \
  -d '{}'
```

## `GET /api/stats`

- **Resposta**: `application/json` com lista de contentores em execução e métricas aproximadas de CPU/memória.
- **Timeout interno**: janela curta no handler; falhas de coleta devolvem `500` sem expor stack ao cliente.

## `GET /api/ws/logs?container=<id|nome>`

- **Upgrade**: WebSocket.
- **Query**: `container` obrigatório (ID ou nome Docker).
- **Origem**: pedidos de browser só com mesma `Host` ou origens listadas em `GODEPLOY_WS_ALLOWED_ORIGINS`.
- **Mensagens**: JSON por linha (`stream`, `line`) conforme implementação em `internal/observability`.

Cliente de referência: `cmd/godeploy-logtail`.
