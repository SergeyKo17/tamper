# AGENTS.md

tamper is a gRPC fault injection proxy. It sits between a client and a gRPC
server, relays every call as raw bytes, and applies the rules from a YAML file
to the calls that match: delaying them, aborting them with a status code, or
damaging the messages in flight. It knows nothing about the schema of what it
relays. Written in Go, shipped as one static binary and a distroless image.

## Where things live

| Path | What it holds |
|---|---|
| `cmd/tamper/` | The only public package: flags, logger, signals |
| `internal/app/` | Wiring — loads the config, builds injectors, assembles the proxy |
| `internal/config/` | YAML loading, validation, the directory watcher behind the reload |
| `internal/fault/` | The five fault types and the dice that decide when they fire |
| `internal/proxy/` | The gRPC server and client, the relay itself, TLS on both legs |
| `demo/` | A mock echo server and a compose stack, not part of the product |
| `docs/` | Reference: config fields, fault semantics, operations |

Nothing but `cmd/tamper` is importable from outside, and that is deliberate.
A facade for embedding the proxy in a Go test may be added later; until then,
do not widen the public surface.

The demo mock belongs to the root module on purpose. It is a package of this
module like any other, so one `go build ./...` covers it and there is no
`go.work` to keep in step. Do not give `demo/echo` a `go.mod` of its own.

Four direct dependencies — fsnotify, grpc, protobuf, yaml — and that is the
intended size. Propose a new one before reaching for it.

## Commands

```bash
gofmt -l .          # gofmt takes paths, not ./...
go vet ./...
go build ./...
make test           # the whole suite
```

```bash
make lint           # golangci-lint over ./...
make vuln           # govulncheck, downloads its database over the network
make snapshot       # the full goreleaser matrix into dist/
make docker         # the image, same build as CI
make demo-proto     # regenerate demo/echo/echopb after editing echo.proto
```

`go test -race ./...` needs a C toolchain, so it may not run everywhere; CI
runs it on every PR.

## Invariants

- The request pump outlives the handler. `forwardRequests` keeps running after
  `handler` returns; this is not a leak, and guarding it with a `WaitGroup`
  deadlocks. Reviews keep reporting it — it has been checked twice.
- A `Proxy` is one-shot. `Close` is idempotent, but a stopped proxy cannot be
  started again; build a new one.
- The watcher subscribes to the directory, never to the file alone: an editor
  that saves by rename would otherwise kill the hot reload for good.
- A reload swaps the rules and the log level, nothing else. A changed
  `listen`, `target` or TLS section needs a restart, and `warnRestartNeeded`
  says so in the log. A new config field has to be added to that list, or it
  will be edited, announced as reloaded, and quietly ignored.
- Fault randomness is not reproducible and will not be made so. Two goroutines
  draw from the same source; a seed was tried and dropped.
- `demo/echo/echopb/` is generated. Change `echo.proto` and run
  `make demo-proto`; never edit the generated files.

## Style

- Comments and identifiers in English. A comment inside a function is two
  lines at most — rationale belongs in the PR description.
- `godot` is on: every comment ends with a period. `revive` requires a doc
  comment on everything exported.
- Wrap errors with context: `fmt.Errorf("read config: %w", err)`.

## Before you commit

Commit messages and PR format follow [CONTRIBUTING.md](CONTRIBUTING.md):
Conventional Commits, the scope naming the package. Run `make lint` and
`make test` first — CI rejects both failures.
