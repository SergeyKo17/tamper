# tamper

[![CI](https://github.com/SergeyKo17/tamper/actions/workflows/ci.yml/badge.svg)](https://github.com/SergeyKo17/tamper/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.26-blue)

gRPC fault injection proxy for testing service resilience.

## How it works

```
Client  →  tamper (proxy)  →  Server
               ↓
         fault injection
         (delay / abort)
```

tamper sits between a gRPC client and server and applies configurable faults —
delays and aborts — selected by method and probability.

It needs no protobuf schemas. Payloads are forwarded as opaque bytes, so any
service works without regenerating anything, and every call is relayed as a
bidirectional stream: unary, client-streaming, server-streaming and
bidirectional methods all pass through unchanged. Request metadata, response
headers and trailers are forwarded in both directions, so authentication and
tracing keep working through the proxy.

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
| `match.method` | Full gRPC method path, e.g. `/package.Service/Method` |
| `fault.type` | `delay` or `abort` |
| `fault.duration` | Delay duration, e.g. `200ms`, `1s` (delay only) |
| `fault.code` | gRPC status code 1–16 (abort only) |
| `fault.msg` | Error message returned to client (abort only) |
| `fault.prob` | Probability 0.0–1.0 that the fault fires |

Every rule whose `match` fits the call is applied, in the order they appear.

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

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
