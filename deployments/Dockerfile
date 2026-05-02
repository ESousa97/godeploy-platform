# syntax=docker/dockerfile:1
# Build godeployd (HTTP API + webhook + proxy wiring). Mount Docker socket at runtime.
FROM golang:1.26-bookworm AS build
WORKDIR /src
ENV GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/godeployd ./cmd/godeployd

FROM debian:bookworm-slim
RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates \
	&& rm -rf /var/lib/apt/lists/*
COPY --from=build /out/godeployd /usr/local/bin/godeployd
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/godeployd"]
