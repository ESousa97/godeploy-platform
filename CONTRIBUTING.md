# Contribuindo

Obrigado por considerar uma contribuição ao godeploy-platform.

## Ambiente de desenvolvimento

- **Go**: versão indicada em `go.mod` (use a mesma família que a CI quando possível).
- **Docker**: Engine local para integrações de `builder`, `scheduler` e observabilidade.
- **Git**: necessário para a pipeline em runtime e para clones nos testes de integração.
- **Ferramentas opcionais**: `golangci-lint`, `govulncheck`, `gosec` (alinhadas ao `Makefile`).

## Estilo de código

- Siga `gofmt` / convenções idiomáticas de Go: [Effective Go](https://go.dev/doc/effective_go) e [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments).
- Comentários de documentação em símbolos exportados: frase completa começando pelo nome; use links `[Tipo]` para o mesmo módulo quando fizer sentido.
- Mensagens de erro e logs voltados a operadores podem permanecer em português, coerente com o código existente, salvo acordo em PR para mudar o idioma.

## Validar antes do PR

```bash
make validate
```

Gate mais próximo da CI (inclui piso de cobertura nos testes `-short`):

```bash
make validate-full
```

Equivalente mínimo:

```bash
make fmt
make vet
make test-short
make build
make lint
```

- **`make test-race`**: corre `go test -race ./...` (em Windows pode exigir `CGO_ENABLED=1`; a CI em Linux usa `-race`).
- **`make test-cover-check`**: testes `-short` com perfil de cobertura e falha se a cobertura total ficar abaixo de `COVER_MIN` (padrão **29%**; ajuste com `make test-cover-check COVER_MIN=30`).
- **Hooks Git**: após clonar, `make install-hooks` passa a usar `.githooks/pre-commit`. Alternativa com [pre-commit](https://pre-commit.com): ficheiro `.pre-commit-config.yaml` na raiz.

Em Windows, se o antivírus bloquear `*.test.exe` em `%TEMP%\go-build*`, os testes em `internal/detector` podem estar omitidos por *build tag*; use WSL, Linux na CI, ou `go test -tags=force_detector_tests ./internal/detector/` após exclusão no Defender (ver README).

## Conventional Commits

Prefira o formato `tipo(escopo opcional): descrição` (imperativo, linha curta):

| Tipo | Uso |
|------|-----|
| `feat` | Nova funcionalidade |
| `fix` | Correção de bug |
| `refactor` | Refatoração sem mudar comportamento |
| `docs` | Apenas documentação |
| `test` | Testes |
| `chore` | Manutenção, dependências, tooling |
| `ci` | Pipelines e automação CI |
| `perf` | Performance |
| `security` | Correções de segurança |

Exemplos: `fix(proxy): tighten dial timeout`, `docs(readme): sync Makefile targets`.

## Processo de PR

- **Branches (repositório)**: em `main`, recomenda-se proteção com revisão obrigatória e *checks* exigidos para o gate principal (`lint`, `verify` no workflow `ci`). O job `vulncheck` (`govulncheck`) corre em paralelo para visibilidade; pode estar configurado como não bloqueante enquanto uma dependência não tiver tag corrigida na base OSV. Evite *force-push* partilhado em ramos que outros reutilizam.
- **Branches**: prefira nomes descritivos, por exemplo `fix/proxy-timeout` ou `docs/readme-api`.
- **Commits**: mensagens claras no imperativo (ver tabela acima); evite commits gigantes difíceis de rever.
- **Descrição**: explique o problema, a solução e o risco de regressão; referencie issues quando existirem.
- **Review**: responda a comentários; mantenha o PR focado num objetivo.

## Releases

- Tags `v*` (ex.: `v0.2.0`) disparam o workflow GitHub Actions `release`, que usa [GoReleaser](https://goreleaser.com) (`.goreleaser.yaml`) para publicar ficheiros por SO/arquitectura (pacote com `godeployd`, `godeploy-tui` e `godeploy-logtail`, mais checksums).
- Validação local da configuração: `goreleaser check`. *Build* de *snapshot* sem publicar: `goreleaser release --snapshot --clean` (útil antes de criar a tag).

## Áreas onde contribuições são bem-vindas

- Testes de integração condicionados a `testing.Short()` e ambientes com Docker.
- Melhorias de segurança operacional (headers, limites, logging) com mudanças pequenas e testáveis.
- Documentação (`README`, `docs/`, comentários godoc) e exemplos reproduzíveis.

## Conduta

Participação sujeita ao [Code of Conduct](CODE_OF_CONDUCT.md).
