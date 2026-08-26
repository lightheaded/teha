# syntax=docker/dockerfile:1
#
# Multi-stage build for the teha server.
#
# The SQLite driver (modernc.org/sqlite) is pure Go, so the build needs no cgo
# and the binary is fully static.
#
#   docker build -t teha:dev .
#   docker build --build-arg VERSION=0.1.0 -t teha:0.1.0 .

FROM golang:1.26-alpine AS build

WORKDIR /src

# Download the modules first, so a source change does not invalidate this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# VERSION reaches the binary, not only the image label, so `teha -version`
# inside a running container names the build that a deployment pinned.
ARG VERSION=dev
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags "-s -w -X main.buildVersion=${VERSION}" -o /out/teha ./cmd/teha

# The health probe. distroless/static holds no shell and no curl, so the image
# carries one small static client that calls /v1/health.
COPY <<'GO' /src/healthcheck/main.go
package main

import (
	"bufio"
	"net"
	"os"
	"strings"
	"time"
)

func main() {
	addr := os.Getenv("TEHA_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8637"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		addr = "127.0.0.1" + strings.TrimPrefix(addr, "0.0.0.0")
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		os.Exit(1)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("GET /v1/health HTTP/1.0\r\nHost: localhost\r\n\r\n")); err != nil {
		os.Exit(1)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, " 200 ") {
		os.Exit(1)
	}
}
GO
RUN cd /src/healthcheck && go mod init healthcheck >/dev/null 2>&1 \
	&& go build -trimpath -ldflags "-s -w" -o /out/healthcheck .

# An empty data directory with the right owner. The final image has no shell,
# so the directory must arrive from this stage.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev
LABEL org.opencontainers.image.title="teha" \
	org.opencontainers.image.description="Self-hosted task manager: API, web app and MCP server in one binary" \
	org.opencontainers.image.source="https://github.com/lightheaded/teha" \
	org.opencontainers.image.version="${VERSION}"

COPY --from=build /out/teha /usr/local/bin/teha
COPY --from=build /out/healthcheck /usr/local/bin/healthcheck
COPY --from=build --chown=65532:65532 /out/data /data

# 65532 is the `nonroot` user of the base image.
USER 65532:65532

ENV TEHA_ADDR=0.0.0.0:8637 \
	TEHA_DB=/data/teha.db

# The database and the attachments live here.
VOLUME ["/data"]
EXPOSE 8637

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
	CMD ["/usr/local/bin/healthcheck"]

ENTRYPOINT ["/usr/local/bin/teha"]
CMD ["serve"]
