# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

ARG NODE_IMAGE=node:24-alpine@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd
ARG GO_IMAGE=golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b
ARG PNPM_VERSION=11.13.1
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=1970-01-01T00:00:00Z

FROM ${NODE_IMAGE} AS web-build
ARG PNPM_VERSION
WORKDIR /src

RUN npm install --global "pnpm@${PNPM_VERSION}"
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./web/
RUN pnpm --dir web install --frozen-lockfile

COPY api ./api
COPY web ./web
RUN pnpm --dir web generate:api && pnpm --dir web build

FROM ${GO_IMAGE} AS go-build
ARG VERSION
ARG COMMIT
ARG BUILD_TIME
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /src/web/dist ./web/dist

RUN mkdir -p /out/data && \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -buildvcs=false -trimpath \
      -ldflags="-s -w -X github.com/AlexKris/sidervia/internal/buildinfo.Version=${VERSION} -X github.com/AlexKris/sidervia/internal/buildinfo.Commit=${COMMIT} -X github.com/AlexKris/sidervia/internal/buildinfo.BuildTime=${BUILD_TIME}" \
      -o /out/sidervia ./cmd/sidervia

FROM ${RUNTIME_IMAGE}
ARG VERSION
ARG COMMIT
ARG BUILD_TIME

LABEL org.opencontainers.image.title="Sidervia" \
      org.opencontainers.image.description="Self-hosted multi-provider AI gateway control plane" \
      org.opencontainers.image.source="https://github.com/AlexKris/sidervia" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}"

COPY --from=go-build --chown=65532:65532 /out/sidervia /sidervia
COPY --from=go-build --chown=65532:65532 /out/data /var/lib/sidervia

USER 65532:65532
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/sidervia", "doctor", "--healthcheck"]
ENTRYPOINT ["/sidervia"]
CMD ["serve"]
