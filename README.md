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

Faults are reloaded from disk while the proxy runs, without dropping
connections.

## Install

```bash
go install github.com/SergeyKo17/tamper/cmd/tamper@latest
```

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

  - match:
      method: "/myapp.UserService/CreateUser"
    fault:
      type: abort
      code: 14
      msg: "service unavailable"
      prob: 0.3

  - match:
      method: "/myapp.UserService/ListUsers"
    fault:
      type: corrupt
      direction: response
      count: 8
      prob: 0.2
```

Run it and point your client at `127.0.0.1:9090`:

```bash
tamper --config tamper.yml
```

Editing `rules` while tamper is running takes effect immediately.

## Configuration

The config has two sides. `listen` faces your clients, where tamper acts as a
server; `target` faces the upstream service, where tamper acts as a client.
Each side is configured independently.

| Field | Description |
|-------|-------------|
| `listen.addr` | Address the proxy listens on (required) |
| `listen.tls` | Serve TLS to clients; plaintext when absent |
| `listen.max_recv_msg_size` | Inbound message limit in bytes; `0` means no limit |
| `listen.max_send_msg_size` | Outbound message limit in bytes; `0` means no limit |
| `target.addr` | Address of the upstream gRPC server (required) |
| `target.tls` | Dial the target over TLS; plaintext when absent |
| `target.max_recv_msg_size` | Inbound message limit in bytes; `0` means no limit |
| `target.max_send_msg_size` | Outbound message limit in bytes; `0` means no limit |
| `logger.level` | Log level: debug, info, warn, error (default: info) |

By default tamper adds no message size limit of its own, leaving the client and
the upstream to enforce theirs.

### Rules

| Field | Description |
|-------|-------------|
| `match.method` | gRPC method path, exact or with `*` wildcards, e.g. `/package.Service/Method` |
| `fault.type` | `delay`, `abort`, `truncate`, `corrupt` or `drop` |
| `fault.prob` | Probability 0.0–1.0 that the fault fires |
| `fault.duration` | Delay duration, e.g. `200ms`, `1s` (delay only) |
| `fault.code` | gRPC status code 1–16 (abort only) |
| `fault.msg` | Error message returned to client (abort only) |
| `fault.direction` | `request`, `response` or `both` (message faults only, default `request`) |
| `fault.size` | Bytes to keep (truncate only) |
| `fault.count` | Bytes to damage (corrupt only, default `1`) |

An unknown key is rejected when the config loads. A typo that is silently
dropped leaves a rule running on zero values, which is worse than a startup
error.

Patterns follow `path.Match`: `*` stands for any run of characters within one
path segment and never crosses a `/`. So `/myapp.UserService/*` covers a whole
service, `/*/GetUser` covers one method name across services, and a lone `*`
covers every call. A pattern is checked when the config loads: one that does
not start with `/`, or that is malformed, is a configuration error rather than
a rule that silently never fires.

Every rule whose `match` fits the call is applied, in the order they appear.
Patterns may overlap, and overlapping rules stack: a `*` rule adding latency
and a rule aborting one method will both fire on that method.

### Message faults

`delay` and `abort` act on the call as a whole, before anything is forwarded.
`truncate`, `corrupt` and `drop` act on each message in flight, and take a
`direction`:

| Direction | Applies to |
|-----------|------------|
| `request` | Messages on the way to the target (the default) |
| `response` | Messages on the way back to the client |
| `both` | Messages on either leg |

A `direction` on `delay` or `abort` is a configuration error: those faults have
no leg to run on.

**`truncate`** keeps the first `size` bytes and throws the rest away. This is
what a receiver sees when a connection dies mid-message: the gRPC frame arrives
intact and announces the shorter length, and the payload inside it does not
parse. A message already at or below `size` passes through untouched — the fault
cuts messages down, it never pads them out.

**`corrupt`** damages `count` bytes chosen at random, leaving the length alone.
Decoding such a message usually fails outright; when it does not, a field
quietly carries a different value, which is the more interesting case to test.
Because the bytes are opaque, which field is hit is not something you choose.

**`drop`** discards a message instead of relaying it. Neither side is told: the
call stays healthy and the stream simply has a hole in it. On a streaming
method that is exactly a lost message. On a unary call it leaves the caller
waiting for an answer that never comes, until its own deadline fires — so give
unary methods a deadline before pointing `drop` at them.

Rules stack here too, in the order written: a `truncate` followed by a `corrupt`
damages what is left after the cut. A `drop` ends the chain, since a message
that will not be sent has nothing left to change.

## TLS

TLS terminates at the proxy: tamper decrypts what the client sends, applies the
configured faults, and encrypts again on its way to the target. The two sides
are separate connections with separate settings, and either can be plaintext.

### Serving TLS to clients

```yaml
listen:
  addr: "127.0.0.1:9090"
  tls:
    cert_file: "certs/tamper.crt"
    key_file: "certs/tamper.key"
```

Both fields are required together. For local testing, a self-signed pair:

```bash
mkdir -p certs
```

```bash
openssl req -x509 -newkey rsa:2048 -nodes -days 365 -keyout certs/tamper.key -out certs/tamper.crt -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
```

The `subjectAltName` matters: gRPC clients verify the name against it and
ignore the common name.

Keep `certs/` out of version control — the key file is a private key, even for
a throwaway certificate. tamper's own `.gitignore` already excludes it.

### Dialing the target over TLS

```yaml
target:
  addr: "api.example.com:443"
  tls:
    ca_file: "certs/upstream-ca.crt"
    server_name: "api.example.com"
```

| Field | Description |
|-------|-------------|
| `ca_file` | Trust anchor for the target certificate; system roots when empty |
| `server_name` | Name checked against the certificate, needed when dialing by IP |
| `insecure_skip_verify` | Disable verification of the target certificate |
| `cert_file` / `key_file` | Client certificate, for targets requiring mutual TLS |

tamper verifies the target certificate once at startup and refuses to start if
it is rejected, so a misconfigured trust anchor surfaces immediately rather
than on the first request. A target that is merely unreachable is reported and
tolerated: it is allowed to come up later.

`insecure_skip_verify` turns off certificate checking entirely — chain, expiry
and name. Traffic stays encrypted, but nothing proves who is on the other end,
so anyone in the middle can impersonate the target. Prefer `server_name` when
only the name is wrong, and `ca_file` when the target uses a private CA.

## Logging

JSON to stdout on every request:

```json
{"time":"2026-09-17T14:38:43","level":"INFO","msg":"request","method":"/myapp.UserService/GetUser","duration":"203.1ms","error":null}
```

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
