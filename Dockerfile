# syntax=docker/dockerfile:1.7
#
# sslsync container image: a static binary on distroless, running as non-root.
#
#   docker run --rm \
#     -v "$PWD/.env:/work/.env:ro" \
#     -v "$PWD/certs:/work/certs:ro" \
#     -v "$HOME/.ssh/known_hosts:/home/nonroot/.ssh/known_hosts:ro" \
#     -v "$PWD/logs:/work/logs" \
#     ghcr.io/ilramdhan/sslsync:latest check
#
# .env must be chmod 600 and owned by uid 65532, or pass `--user $(id -u)`.

ARG GO_VERSION=1.26.9

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS TARGETARCH VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/ilramdhan/sslsync/internal/app.Version=${VERSION}" \
      -o /out/sslsync ./cmd/sslsync

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="sslsync" \
      org.opencontainers.image.description="Renew SSL certificates on many servers over SSH, verified, with automatic rollback" \
      org.opencontainers.image.source="https://github.com/ilramdhan/sslsync" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/sslsync /usr/local/bin/sslsync
WORKDIR /work
ENV SSLSYNC_KNOWN_HOSTS=/home/nonroot/.ssh/known_hosts
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/sslsync"]
CMD ["help"]
