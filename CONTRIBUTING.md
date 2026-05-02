# Contributing

Thank you for considering a contribution to godeploy-platform.

## Development environment

- **Go**: version pinned in `go.mod` (use the same family as CI when possible).
- **Docker**: local Engine for `builder`, `scheduler`, and observability integrations.
- **Git**: required for the runtime pipeline and for clones in integration tests.
- **Optional tools**: `golangci-lint`, `govulncheck`, `gosec` (aligned with the `Makefile`).

## Code style

- Follow `gofmt` / idiomatic Go: [Effective Go](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments).
- Documentation comments on exported symbols: full sentence starting with the name; use `[Type]` links within the same module when helpful.
- Error messages and operator-facing logs are in **English**, consistent with the codebase.

## Validate before opening a PR

```bash
make validate
```

Closest gate to CI (includes coverage floor on `-short` tests):

```bash
make validate-full
```

Minimum equivalent:

```bash
make fmt
make vet
make test-short
make build
make lint
```

- **`make test-race`**: runs `go test -race ./...` (on Windows you may need `CGO_ENABLED=1`; Linux CI uses `-race`).
- **`make test-cover-check`**: `-short` tests with a coverage profile; fails if total coverage is below `COVER_MIN` (default **29%**; override with `make test-cover-check COVER_MIN=30`).
- **Git hooks**: after clone, `make install-hooks` uses `.githooks/pre-commit`. Alternative: [pre-commit](https://pre-commit.com) with `.pre-commit-config.yaml` at the repo root.

On Windows, if antivirus blocks `*.test.exe` under `%TEMP%\go-build*`, tests in `internal/detector` may be skipped by build tag; use WSL, Linux CI, or `go test -tags=force_detector_tests ./internal/detector/` after Defender exclusions (see README).

## Conventional Commits

Prefer `type(optional scope): description` (imperative, short first line):

| Type | Use |
|------|-----|
| `feat` | New feature |
| `fix` | Bug fix |
| `refactor` | Refactor without behaviour change |
| `docs` | Documentation only |
| `test` | Tests |
| `chore` | Maintenance, dependencies, tooling |
| `ci` | CI pipelines and automation |
| `perf` | Performance |
| `security` | Security fixes |

Examples: `fix(proxy): tighten dial timeout`, `docs(readme): sync Makefile targets`.

## PR process

- **Branches (repository)**: on `main`, use branch protection with required review and checks for the main gate (`lint`, `verify` in the `ci` workflow). The `vulncheck` job (`govulncheck`) runs in parallel for visibility; it may be non-blocking while a dependency lacks an OSV fix tag on the module. Avoid shared force-push on branches others reuse.
- **Branches**: prefer descriptive names, e.g. `fix/proxy-timeout` or `docs/readme-api`.
- **Commits**: clear imperative messages (see table above); avoid huge commits that are hard to review.
- **Description**: explain the problem, solution, and regression risk; reference issues when they exist.
- **Review**: reply to comments; keep the PR focused on one goal.

## Releases

- Tags `v*` (e.g. `v0.2.0`) trigger the GitHub Actions `release` workflow, which uses [GoReleaser](https://goreleaser.com) (`.goreleaser.yaml`) to publish artifacts per OS/architecture (bundle with `godeployd`, `godeploy-tui`, and `godeploy-logtail`, plus checksums).
- Validate config locally: `goreleaser check`. Snapshot build without publishing: `goreleaser release --snapshot --clean` (useful before tagging).

## Areas where contributions are welcome

- Integration tests gated on `testing.Short()` and environments with Docker.
- Small, testable operational security improvements (headers, limits, logging).
- Documentation (`README`, `docs/`, godoc comments) and reproducible examples.

## Code of conduct

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).
