# Advanced setup

## Environment variables

Copy `.env.example` to `.env` and export variables in your shell before `make run` or `docker compose`, because the Go runtime does not read `.env` natively.

See `.env.example` for the full list of `GODEPLOY_*` knobs.

`deployments/docker-compose.yml` mounts the Docker socket and a volume for `GODEPLOY_DB`. Adjust ports and secrets before exposing anything to the Internet.

## Local validation

```bash
make validate        # fmt, vet, lint, tests, build
make validate-full   # adds -short tests + coverage floor + build
```

Coverage gate on `-short` tests (same idea as `validate-full` in the Makefile):

```bash
make test-cover-check
# or with a custom floor:
make test-cover-check COVER_MIN=30
```

## Linting

Includes `golangci-lint`; install a version compatible with `.golangci.yml` (version format `2`). The CI workflow also runs explicit `goimports` in the format job.

## Windows / antivirus

The `internal/detector` package skips test binaries on Windows by default to avoid `*.test.exe` locks from antivirus. To run that package’s tests on Windows, use WSL or apply the `force_detector_tests` build tag with Defender exclusions (see README).

## Production proxy

The reverse proxy in `internal/proxy` typically listens on `:80` behind a TLS terminator. The `godeployd` daemon does not configure TLS natively; place it behind nginx, Caddy, or a cloud load balancer.
