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

tamper sits between a gRPC client and server, intercepting all requests without requiring protobuf schemas. It applies configurable faults — delays and aborts — based on method matching and probability.

## Install

```bash
go install github.com/SergeyKo17/tamper/cmd/tamper@latest
```

## Quick start

Create a config file `tamper.yml`:

```yaml
listen: "127.0.0.1:9090"
target: "127.0.0.1:50051"

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

Run:

```bash
tamper --config tamper.yml
```

## Configuration

| Field | Description |
|-------|-------------|
| `listen` | Address the proxy listens on (required) |
| `target` | Address of the upstream gRPC server (required) |
| `logger.level` | Log level: debug, info, warn, error (default: info) |

### Rules

| Field | Description |
|-------|-------------|
| `match.method` | Full gRPC method path, e.g. `/package.Service/Method` |
| `fault.type` | `delay` or `abort` |
| `fault.duration` | Delay duration, e.g. `200ms`, `1s` (delay only) |
| `fault.code` | gRPC status code 1–16 (abort only) |
| `fault.msg` | Error message returned to client (abort only) |
| `fault.prob` | Probability 0.0–1.0 that the fault fires |

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
