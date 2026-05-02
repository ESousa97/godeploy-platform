package builder

import (
	"fmt"
	"strings"

	"godeploy-platform/internal/detector"
)

func dockerfileTemplate(rt detector.Runtime) (string, error) {
	switch rt {
	case detector.RuntimeGo:
		return strings.TrimSpace(`
# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# go build -o <file> ./... fails with multiple main packages; pick one under ./cmd/... or the repo root.
RUN set -eux; \
	cd /src; \
	export CGO_ENABLED=0 GOOS=linux GOARCH=amd64; \
	mains=$(go list -f '{{if eq .Name "main"}}{{.ImportPath}} {{end}}' ./cmd/... 2>/dev/null || true); \
	set -- $mains; \
	if [ "$#" -eq 1 ] && [ -n "$1" ]; then \
		go build -trimpath -ldflags="-s -w" -o /out/app "$1"; \
	elif [ "$#" -gt 1 ]; then \
		for want in godeployd server api main app; do \
			for p in "$@"; do \
				case "$p" in */cmd/$want) go build -trimpath -ldflags="-s -w" -o /out/app "$p"; exit 0 ;; esac; \
			done; \
		done; \
		echo "godeploy: varios pacotes main em ./cmd/...; defina um Dockerfile na raiz do repositorio" >&2; exit 1; \
	else \
		mains=$(go list -f '{{if eq .Name "main"}}{{.ImportPath}} {{end}}' . 2>/dev/null || true); \
		set -- $mains; \
		if [ "$#" -eq 1 ] && [ -n "$1" ]; then \
			go build -trimpath -ldflags="-s -w" -o /out/app "$1"; \
		else \
			echo "godeploy: nenhum pacote main em ./cmd/... nem na raiz; defina um Dockerfile na raiz" >&2; exit 1; \
		fi; \
	fi

# Imagem root (nao :nonroot) para permitir GODEPLOY_BIND_DOCKER_SOCK ao self-host do godeployd.
FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/app /app/app
ENV GODEPLOY_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/app/app"]
`) + "\n", nil

	case detector.RuntimeNodeJS:
		return strings.TrimSpace(`
# syntax=docker/dockerfile:1
FROM node:20-alpine AS deps
WORKDIR /app
COPY package*.json ./
RUN npm ci

FROM node:20-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules /app/node_modules
COPY . .
RUN npm run build --if-present

FROM node:20-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
COPY --from=deps /app/node_modules /app/node_modules
COPY . .
EXPOSE 3000
CMD ["npm","start"]
`) + "\n", nil

	case detector.RuntimePython:
		return strings.TrimSpace(`
# syntax=docker/dockerfile:1
FROM python:3.12-slim AS runtime
WORKDIR /app
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
COPY requirements.txt* pyproject.toml* Pipfile* Pipfile.lock* /app/
RUN python -m pip install --no-cache-dir --upgrade pip && \
    if [ -f requirements.txt ]; then pip install --no-cache-dir -r requirements.txt; fi
COPY . .
EXPOSE 8000
CMD ["python","-m","http.server","8000"]
`) + "\n", nil

	case detector.RuntimeStatic:
		return strings.TrimSpace(`
# syntax=docker/dockerfile:1
FROM nginx:alpine
COPY . /usr/share/nginx/html
EXPOSE 80
`) + "\n", nil

	default:
		return "", fmt.Errorf("runtime nao suportado para template: %s", rt)
	}
}
