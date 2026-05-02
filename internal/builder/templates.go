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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/app ./...

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/app /app/app
EXPOSE 8080
USER nonroot:nonroot
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
