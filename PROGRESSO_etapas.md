# Progresso do plano (`plan_go.md`)

Este ficheiro regista **onde o trabalho parou** para continuar em etapas. O plano completo está em `plan_go.md` na raiz (etapas oficiais **0–6**). A **etapa 7** é extensão acordada *após* o plano: automação e *badges* opcionais descritos em `plan_go.md` §5.1 / §5.3 e no antigo «próximo passo» deste ficheiro.

---

## Estado atual

| Etapa | Título | Estado |
|-------|--------|--------|
| **0** | Inventário técnico e baseline | **Concluída** |
| **1** | Organização, padrões e higiene do repo | **Concluída** |
| **2** | Código, arquitetura e segurança | **Concluída** |
| **3** | Documentação nível A+ | **Concluída** |
| **4** | Qualidade, testes e automação local | **Concluída** |
| **5** | CI/CD, governança e profissionalismo | **Concluída** |
| **6** | Entrega final e relatório | **Concluída** |
| **7** | Pós-plano: auditoria agendada, releases e CodeFactor | **Concluída** |

---

## Onde parou (última sessão)

- **Etapa 7** (2026-05-02): extensão pós-`plan_go.md` §5.1 (workflows adicionais) e §5.3 (badge CodeFactor).
  - **`.github/workflows/security-schedule.yml`**: `govulncheck` semanal (segunda 06:00 UTC) e `workflow_dispatch`; `continue-on-error: true` alinhado ao job `vulncheck` da CI enquanto dependências não tiverem correcção OSV publicada no módulo.
  - **`.goreleaser.yaml`** + **`.github/workflows/release.yml`**: publicação em GitHub Releases ao empurrar tag `v*`; pacotes por SO/arquitectura com os três binários (`godeployd`, `godeploy-tui`, `godeploy-logtail`), `formats` / `ids` conforme GoReleaser v2 (sem propriedades deprecadas); `goreleaser check` validado localmente.
  - **`README.md`**: badge [CodeFactor](https://www.codefactor.io) para `github.com/esousa97/godeploy-platform` (registar o repositório em CodeFactor se o badge ainda não reflectir nota).
  - **`CONTRIBUTING.md`**: secção **Releases** com fluxo de tags e comandos `goreleaser check` / *snapshot*.
  - **Validação local:** `go build ./...`, `go test -short ./...`, `goreleaser check` com sucesso neste clone.

- **Etapa 6** (2026-05-02): entrega final e relatório (`plan_go.md` *# ETAPA 6*).
  - **`FINAL.md`** (raiz): relatório executivo com resumo antes/depois, changelog por etapas 0–5, comandos exactos, decisões arquitecturais, riscos remanescentes, avaliação simulada A+, posicionamento para revisores e checklist 6.2 com estado dos itens. Ficheiro **local** — está em `.gitignore` (documento interno, alinhado ao plano).
  - **`.github/dependabot.yml`**: Dependabot para `gomod` (`/`), `github-actions` (`/`) e `docker` (`/deployments`), fechando o item da checklist final do plano.
  - **Validação local:** `go mod tidy`, `go mod verify`, `go build ./...`, `go vet ./...`, `go test -short ./...` executados com sucesso neste clone.

- **Etapa 5** (2026-05-02): CI/CD, governança e profissionalismo.
  - **`.github/workflows/ci.yml`**: `permissions: contents: read`; `concurrency` para cancelar execuções obsoletas em PRs; job **`lint`** só com `golangci-lint`; job **`vulncheck`** com `govulncheck` dedicado (`continue-on-error: true` enquanto `github.com/docker/docker` não tiver tag de módulo corrigida para OSVs reportadas — o scan continua visível nos checks); job **`verify`** com matriz Go **`1.25.0`** e **`stable`** (`fail-fast: false`); upload de artefacto **`coverage-<versão>.txt`** após testes com `-race`, `-short` e piso de cobertura (29%).
  - **`CONTRIBUTING.md`**: recomendação explícita de ramo `main` protegido com checks `lint`, `vulncheck`, `verify`.
  - **Templates GitHub** (já presentes): `.github/ISSUE_TEMPLATE/*`, `.github/PULL_REQUEST_TEMPLATE.md`, `CODEOWNERS`, `SECURITY.md`.

- **Etapa 4** (2026-05-02): qualidade, testes, cobertura e lint sem erros.
  - **`.golangci.yml`**: alinhado ao plano (errcheck, staticcheck, revive, gocritic, godot, exhaustive, sqlclosecheck, etc.); `gocritic` com `rangeValCopy` desactivado (ruído vs. cópias em structs Docker).
  - **`cmd/godeployd`**: `run()` com código de saída em vez de `os.Exit` no meio (defers e `gocritic` exitAfterDefer); fecho explícito da DB se o cliente Docker falhar (caminho corrigido antes do refactor).
  - **`internal/proxy`**: `http.Transport` partilhado em `ServeHTTP` (evita fuga de ligações / bloqueios em testes Windows); `CloseIdleConnections` no shutdown de `Run`; teste de hot-reload determinístico com `reloadIfChanged(ctx, true)`.
  - **Testes novos**: `internal/pipeline/pipeline_test.go` (validação `New`, helpers, `waitHTTP200`); `internal/platform/iox/iox_test.go`; `internal/platform/sqlpool/sqlpool_test.go`; `internal/webhook/example_test.go`; observability com `testing.Short()` para não depender do Docker em `make test-short` / CI `-short`.
  - **`Makefile` `test-cover-check`**: ordem de flags `-covermode` antes de `-coverprofile` (compatível com shells que partem `=ficheiro`).
  - **Cobertura**: `go test -short` + piso **≥29%** a passar (~30% após novos testes).
  - **Nota Windows**: `go test -race` exige **CGO** e compilador C (p.ex. gcc); sem isso, usar CI ou instalar toolchain; o hook `.githooks/pre-commit` já faz fallback para `go test -short` sem `-race` quando `CGO_ENABLED=0`.

- **Etapa 3** (2026-05-02): documentação e godoc (ver histórico anterior).
- **Etapas 0–2**: ver entradas anteriores no histórico de commit ou na secção acima na tabela.

---

## Próximo passo recomendado

Plano **0–7 concluído** (0–6 no `plan_go.md` + etapa 7 de extensão). Manutenção: seguir `CONTRIBUTING.md` (incluindo **Releases**), rever PRs do Dependabot, e actualizar `CHANGELOG.md` / docs quando houver alterações de produto. Opcional: confirmar o projecto em [CodeFactor](https://www.codefactor.io) para o badge do README mostrar a nota.

---

## Como atualizar este ficheiro

Após cada etapa: marque a linha da tabela como **Concluída**, actualize **Onde parou** com data e ficheiros tocados, e preencha **Próximo passo** com o número da etapa seguinte.
