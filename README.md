# godeploy-platform

Projeto de estudo/Aprendizado: uma “mini PaaS” em Go que ajuda a **detectar** o tipo de app, **buildar** uma imagem Docker (com Dockerfile do usuário ou template) e **fazer deploy** em containers Docker com um blue-green básico.

## O que existe hoje

- **`internal/detector`**: detecta o runtime do projeto (Dockerfile, Go, Node.js, Python ou estático) a partir de arquivos marcadores.
- **`internal/builder`**: resolve/gera um Dockerfile (template embutido quando necessário) e faz `ImageBuild` via Docker Engine, gerando uma tag com timestamp + commit curto.
- **`internal/scheduler`**: cria/garante uma network dedicada e faz deploy de uma imagem em container com limites de CPU/RAM e blue-green simples (sobe novo, espera `running`, depois remove o antigo).

## Requisitos

- Go instalado (compatível com o `go.mod`)
- Docker Engine rodando localmente (para os testes de integração e para `builder/scheduler`)

## Como rodar os testes

Rodar tudo:

```bash
go test ./...
```

Rodar apenas unit tests (pular integrações):

```bash
go test -short ./...
```

## Notas de design (intencionalmente simples)

- Este repositório é **objeto de estudo**: o foco é clareza e evolução incremental.
- Os pacotes estão em `internal/` para deixar explícito que a API ainda não é estável/pública.

## Roadmap (idéias)

- CLI em `cmd/godeploy` (ex.: `detect`, `build`, `deploy`)
- Suporte a variáveis de ambiente e healthcheck no deploy
- Estratégias de rollout mais robustas (readiness, retries, rollback)
- Publicação de imagens em registry (GHCR) e deploy apontando para tags imutáveis

