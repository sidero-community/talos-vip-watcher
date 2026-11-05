# syntax=docker/dockerfile:1

# The binary is cross-compiled on the build platform, so no emulation is needed.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY *.go ./

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/talos-vip-watcher .

# Nothing but the CA bundle and the binary. The process runs as root: it needs NET_ADMIN to
# manage the dummy interface, and a non-root user would lose the capability on exec.
FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/talos-vip-watcher /talos-vip-watcher

ENTRYPOINT ["/talos-vip-watcher"]
