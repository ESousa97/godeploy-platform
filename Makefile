.PHONY: help fmt vet test test-short test-race test-cover test-cover-check install-hooks tidy build build-daemon build-tui build-logtail clean vulncheck sec lint all validate validate-full generate docker-build run

GO ?= go

BIN_DIR := bin
DAEMON := $(BIN_DIR)/godeployd
TUI := $(BIN_DIR)/godeploy-tui
LOGTAIL := $(BIN_DIR)/godeploy-logtail
COVERAGE_FILE := coverage.txt
# Minimum total statement coverage for `make test-cover-check` (see PLAN_PROGRESS.md, stage 4).
COVER_MIN ?= 29
DOCKER_IMAGE ?= godeployd:local

## help: Show available targets
help:
	@printf "%s\n" "Targets:"; \
	grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | sort

## fmt: Format code (gofmt + goimports, same toolchain as CI)
fmt:
	gofmt -w -s .
	$(GO) run golang.org/x/tools/cmd/goimports@latest -w .

## vet: Run go vet
vet:
	$(GO) vet ./...

## test: Run all tests
test:
	$(GO) test ./...

## test-short: Run unit tests only (skip integration via -short)
test-short:
	$(GO) test -short ./...

## test-race: Run tests with race detector
test-race:
	$(GO) test -race ./...

## test-cover: Run tests and write coverage.html
test-cover:
	$(GO) test -coverprofile $(COVERAGE_FILE) -covermode atomic ./...
	$(GO) tool cover -html $(COVERAGE_FILE) -o coverage.html

## test-cover-check: Run short tests with coverage and fail if total is below COVER_MIN (default 29%)
test-cover-check:
	$(GO) test -short -covermode atomic -coverprofile $(COVERAGE_FILE) ./...
	$(GO) tool cover -func $(COVERAGE_FILE) | $(GO) run ./scripts/coveragefloor $(COVER_MIN)

## install-hooks: Point this repo at .githooks (run once per clone)
install-hooks:
	git config core.hooksPath .githooks

## tidy: Normalize go.mod/go.sum
tidy:
	$(GO) mod tidy

## all: fmt, vet, test, build (no lint)
all: fmt vet test build

## validate: fmt, vet, lint, test, build (CI-style local gate)
validate: fmt vet lint test build

## validate-full: validate plus coverage floor (short tests only)
validate-full: fmt vet lint test-short test-cover-check build

## build: Build all binaries
build: build-daemon build-tui build-logtail

## build-daemon: Build godeployd
build-daemon:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(DAEMON) ./cmd/godeployd

## build-tui: Build godeploy-tui
build-tui:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(TUI) ./cmd/godeploy-tui

## build-logtail: Build godeploy-logtail
build-logtail:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(LOGTAIL) ./cmd/godeploy-logtail

## run: Run godeployd (loads env vars from your shell; configure GODEPLOY_* before starting)
run:
	$(GO) run ./cmd/godeployd

## clean: Remove build artifacts
clean:
	$(GO) clean -cache -testcache
	rm -rf $(BIN_DIR) $(COVERAGE_FILE) coverage.html

## lint: Run golangci-lint (requires golangci-lint installed)
lint:
	golangci-lint run ./...

## vulncheck: Run govulncheck (via scripts/govulncheckci; same policy as CI)
vulncheck:
	$(GO) run ./scripts/govulncheckci ./...

## sec: Run gosec (static security)
sec:
	gosec ./...

## generate: Run go generate
generate:
	$(GO) generate ./...

## docker-build: Build godeployd image (requires Docker)
docker-build:
	docker build -t $(DOCKER_IMAGE) -f deployments/Dockerfile .
