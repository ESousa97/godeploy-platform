# Setup avançado

## Variáveis de ambiente

Copie `.env.example` para `.env` e exporte as variáveis no shell antes de `make run` ou `docker compose`, porque o runtime Go não lê `.env` nativamente.

## Docker Compose

O ficheiro `deployments/docker-compose.yml` monta o socket Docker e um volume para `GODEPLOY_DB`. Ajuste portas e secrets antes de expor à Internet.

## CI local

Gate principal (espelha a maior parte do workflow GitHub Actions):

```bash
make validate
```

Gate com piso de cobertura nos testes `-short` (como `validate-full` no Makefile):

```bash
make validate-full
```

Inclui `golangci-lint`; instale a versão compatível com `.golangci.yml` (formato versão `2`). O workflow de CI usa também `goimports` explícito no job de formato.

## Windows e testes

O pacote `internal/detector` omite ficheiros de teste em Windows por defeito para evitar bloqueio de `*.test.exe` por antivírus. Para correr testes desse pacote no Windows, use WSL ou aplique a *build tag* `force_detector_tests` com exclusões no Defender (ver README).

## Proxy em produção

O reverse proxy em `internal/proxy` escuta tipicamente em `:80` atrás de um terminador TLS. O daemon `godeployd` não configura TLS nativamente; coloque-o atrás de nginx, Caddy ou cloud load balancer.
