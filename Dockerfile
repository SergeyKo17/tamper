# syntax=docker/dockerfile:1

# The build stage stays on the host architecture and cross-compiles from there.
# With CGO off that costs nothing, and it keeps the compiler out of emulation
# when the target is not what the builder runs on.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /src

# Module files first: this layer survives every change that does not touch
# them, which is most changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/tamper ./cmd/tamper

# static holds no libc, which is what a CGO-free binary wants, and it already
# carries the CA certificates tamper needs to dial a TLS target. The nonroot
# variant runs as uid 65532, so a listener must stay above port 1024.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/tamper /tamper

# Documentation only. Which port is bound comes from the configuration.
EXPOSE 9090

# The configuration is mounted, never baked in. Mount the directory rather than
# the file alone: a single-file bind mount hides the rename the watcher reads as
# a reload, and hot reload stops working without a word.
ENTRYPOINT ["/tamper"]
CMD ["-config", "/etc/tamper/tamper.yml"]
