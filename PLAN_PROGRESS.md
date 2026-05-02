# Plan progress (`plan_go.md`)

This file records **where work stopped** so you can continue in stages. The full plan is in `plan_go.md` at the repository root (official stages **0–6**). **Stage 7** is an agreed extension *after* the plan: optional automation and badges described in `plan_go.md` §5.1 / §5.3 and in the former “next step” section of this file.

---

## Current status

| Stage | Title | Status |
|-------|-------|--------|
| **0** | Technical inventory and baseline | **Done** |
| **1** | Organisation, standards, and repo hygiene | **Done** |
| **2** | Code, architecture, and security | **Done** |
| **3** | A+ documentation | **Done** |
| **4** | Quality, tests, and local automation | **Done** |
| **5** | CI/CD, governance, and polish | **Done** |
| **6** | Final delivery and report | **Done** |
| **7** | Post-plan: scheduled audit, releases, and CodeFactor | **Done** |

---

## Where it stopped (last session)

- **Stage 7** (2026-05-02): post-`plan_go.md` §5.1 (extra workflows) and §5.3 (CodeFactor badge).
  - **`.github/workflows/security-schedule.yml`**: weekly `govulncheck` (Monday 06:00 UTC) and `workflow_dispatch`; `continue-on-error: true` aligned with the CI `vulncheck` job while dependencies lack a published OSV fix on the module.
  - **`.goreleaser.yaml`** + **`.github/workflows/release.yml`**: publish to GitHub Releases when pushing a `v*` tag; per-OS/architecture packages with all three binaries (`godeployd`, `godeploy-tui`, `godeploy-logtail`), `formats` / `ids` per GoReleaser v2 (no deprecated properties); `goreleaser check` validated locally.
  - **`README.md`**: [CodeFactor](https://www.codefactor.io) badge for `github.com/esousa97/godeploy-platform` (register the repo on CodeFactor if the badge does not yet show a grade).
  - **`CONTRIBUTING.md`**: **Releases** section with tag flow and `goreleaser check` / snapshot commands.
  - **Local validation:** `go build ./...`, `go test -short ./...`, `goreleaser check` succeeded on this clone.

- **Stage 6** (2026-05-02): final delivery and report (`plan_go.md` *# STAGE 6*).
  - **`FINAL.md`** (root): executive report with before/after summary, changelog by stages 0–5, exact commands, architectural decisions, residual risks, simulated A+ evaluation, reviewer positioning, and checklist 6.2 with item status. **Local** file — it is in `.gitignore` (internal document, aligned with the plan).
  - **`.github/dependabot.yml`**: Dependabot for `gomod` (`/`), `github-actions` (`/`), and `docker` (`/deployments`), closing the final checklist item from the plan.
  - **Local validation:** `go mod tidy`, `go mod verify`, `go build ./...`, `go vet ./...`, `go test -short ./...` succeeded on this clone.

- **Stage 5** (2026-05-02): CI/CD, governance, and professionalism.
  - **`.github/workflows/ci.yml`**: `permissions: contents: read`; `concurrency` to cancel stale runs on PRs; **`lint`** job with `golangci-lint` only; **`vulncheck`** job with dedicated `govulncheck` (`continue-on-error: true` while `github.com/docker/docker` has no module tag fixing reported OSVs — the scan remains visible in checks); **`verify`** job with Go matrix **`1.25.0`** and **`stable`** (`fail-fast: false`); artifact upload **`coverage-<version>.txt`** after tests with `-race`, `-short`, and coverage floor (29%).
  - **`CONTRIBUTING.md`**: explicit recommendation to protect `main` with required checks `lint`, `vulncheck`, `verify`.
  - **GitHub templates** (already present): `.github/ISSUE_TEMPLATE/*`, `.github/PULL_REQUEST_TEMPLATE.md`, `CODEOWNERS`, `SECURITY.md`.

- **Stage 4** (2026-05-02): quality, tests, coverage, and lint clean.
  - **`.golangci.yml`**: aligned with the plan (errcheck, staticcheck, revive, gocritic, godot, exhaustive, sqlclosecheck, etc.); `gocritic` with `rangeValCopy` disabled (noise vs. copies in Docker structs).
  - **`cmd/godeployd`**: `run()` exits via return code instead of `os.Exit` mid-flow (defers and `gocritic` exitAfterDefer); explicit DB close if the Docker client fails (path fixed before refactor).
  - **`internal/proxy`**: shared `http.Transport` in `ServeHTTP` (avoids connection leaks / hangs on Windows tests); `CloseIdleConnections` on `Run` shutdown; deterministic hot-reload test with `reloadIfChanged(ctx, true)`.
  - **New tests**: `internal/pipeline/pipeline_test.go` (`New` validation, helpers, `waitHTTP200`); `internal/platform/iox/iox_test.go`; `internal/platform/sqlpool/sqlpool_test.go`; `internal/webhook/example_test.go`; observability respects `testing.Short()` so `make test-short` / CI `-short` do not depend on Docker.
  - **`Makefile` `test-cover-check`**: flag order `-covermode` before `-coverprofile` (compatible with shells that split `=file`).
  - **Coverage**: `go test -short` + floor **≥29%** passing (~30% after new tests).
  - **Windows note**: `go test -race` needs **CGO** and a C compiler (e.g. gcc); without it, use CI or install a toolchain; `.githooks/pre-commit` already falls back to `go test -short` without `-race` when `CGO_ENABLED=0`.

- **Stage 3** (2026-05-02): documentation and godoc (see earlier history).
- **Stages 0–2**: see previous commit history or the table above.

---

## Recommended next step

Plan **0–7 complete** (0–6 in `plan_go.md` + stage 7 extension). Maintenance: follow `CONTRIBUTING.md` (including **Releases**), review Dependabot PRs, and update `CHANGELOG.md` / docs when product behaviour changes. Optional: register the project on [CodeFactor](https://www.codefactor.io) so the README badge shows a grade.

---

## How to update this file

After each stage: mark the table row **Done**, update **Where it stopped** with the date and files touched, and set **Recommended next step** to the following stage number.
