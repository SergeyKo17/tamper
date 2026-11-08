# tamper

[![CI](https://github.com/SergeyKo17/tamper/actions/workflows/ci.yml/badge.svg)](https://github.com/SergeyKo17/tamper/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.26-blue)

gRPC fault injection proxy for testing service resilience.

## How it works

```
Client  →  tamper (proxy)  →  Server
               ↓
         fault injection
         (delay / abort / truncate / corrupt / drop)
```

tamper sits between a gRPC client and server and applies configurable faults,
selected by method and probability. Faults come at two levels: a call can be
delayed or aborted outright, and the messages flowing through it can be cut
short, damaged or swallowed.

It needs no protobuf schemas. Payloads are forwarded as opaque bytes, so any
service works without regenerating anything, and every call is relayed as a
bidirectional stream: unary, client-streaming, server-streaming and
bidirectional methods all pass through unchanged. Request metadata, response
headers and trailers are forwarded in both directions, so authentication and
tracing keep working through the proxy.

Message faults work on those same opaque bytes. That is what makes them work
anywhere without a schema, and it is also their limit: tamper damages a message,
it does not reach into a field. Aiming a fault at one field needs the protobuf
schema and is not part of this release.

Faults are reloaded while the proxy runs, without dropping connections, either
when the file changes or on a signal. See
[Reloading the configuration](OPERATIONS.md#reloading-the-configuration).

## Install

### Container

```bash
docker pull ghcr.io/sergeyko17/tamper:0.1.0
```

Pin the exact version in a compose file or a CI service. A patch tag such as
`0.1.0` keeps pointing at the image it was published with, while `latest` and
`0.1` move on to the next release.

### Binary

Download the archive for your platform from the
[releases page](https://github.com/SergeyKo17/tamper/releases) and unpack it.
Every archive carries the binary, the documentation and `tamper.example.yml`;
`checksums.txt` next to them verifies the download. Linux, macOS and Windows
are covered, on amd64 and arm64.

### From source

```bash
go install github.com/SergeyKo17/tamper/cmd/tamper@v0.1.0
```

A binary built this way reports its version, which a build from a plain
`go build` cannot: the version is stamped in at link time.

## Quick start

Create `tamper.yml`:

```yaml
listen:
  addr: "127.0.0.1:9090"

target:
  addr: "127.0.0.1:50051"

logger:
  level: "info"

rules:
  - match:
      method: "/myapp.UserService/GetUser"
    fault:
      type: delay
      duration: 200ms
      prob: 0.5
```

Run it and point your client at `127.0.0.1:9090`:

```bash
tamper --config tamper.yml
```

Half the calls to `GetUser` now arrive 200ms late. Editing `rules` while tamper
is running takes effect immediately.

Five fault types are available:

| Type | What it does to a call |
|------|------------------------|
| `delay` | Holds the call before anything is forwarded |
| `abort` | Fails the call with a gRPC status code of your choice |
| `truncate` | Cuts messages short, so they no longer parse |
| `corrupt` | Damages bytes inside a message, leaving its length alone |
| `drop` | Swallows a message, telling neither side |

Each takes a probability and a method pattern, and rules may overlap. The
fields, the matching and what stacking means are in [FAULTS.md](FAULTS.md).

## Documentation

- [CONFIGURATION.md](CONFIGURATION.md) — every config field, TLS on both sides,
  and what the logs contain
- [FAULTS.md](FAULTS.md) — rule matching and the five fault types in detail
- [OPERATIONS.md](OPERATIONS.md) — reloading the configuration and stopping the
  proxy

## Scope and limits

tamper belongs in a test environment, next to the client it is breaking traffic
for. Two things decide that, and neither is a bug:

- **Nothing authenticates on `listen`.** Whatever reaches the port gets its
  calls relayed to the target, so a listener open to the network is an open
  relay into the backend, around everything that used to stand in front of the
  service. Keep the address on loopback — `127.0.0.1:9090`, as the examples
  have it — or inside a network you trust, and prefer a sidecar next to one
  client over a shared endpoint. A container has to bind `0.0.0.0` to be
  reachable at all, so publish the port to loopback instead:
  `-p 127.0.0.1:9090:9090`.
- **`insecure_skip_verify` drops verification of the target certificate.** It
  exists for a self-signed staging cert, and it is a hole in that connection
  everywhere else.

So: not between real users and a live service. There is no flag or
configuration that changes this — the tool exists to make calls fail.

Found a security problem in tamper itself? Open an issue; there is no private
channel yet.

## Use cases

- **Resilience testing** — verify that your services handle slow or failing dependencies gracefully
- **Load balancer validation** — place tamper in front of one backend and check that the balancer retries, fails over, or shifts traffic to healthy instances
- **Timeout tuning** — inject delays to find the right timeout values before they bite in production
- **Chaos engineering** — controlled fault injection in staging environments
- **Malformed input handling** — truncate or corrupt payloads and check that deserialization failures are caught, logged and reported rather than crashing a handler

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
