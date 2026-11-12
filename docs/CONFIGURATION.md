# Configuration

The config has two sides. `listen` faces your clients, where tamper acts as a
server; `target` faces the upstream service, where tamper acts as a client.
Each side is configured independently.

## Listen and target

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

Fault rules live under `rules`. Their fields, the method patterns they match on
and the way overlapping rules stack are in [FAULTS.md](FAULTS.md).

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
{"time":"2026-09-17T14:38:43","level":"INFO","msg":"request","call":42,"method":"/myapp.UserService/GetUser","duration":"203.1ms","error":null}
```

`call` numbers the calls passing through, so the lines one call leaves behind
can be told apart from those of the calls running beside it.

At `debug` level every rule that fires is reported as well. A call-level fault
is reported as it happens; message faults are counted and reported once per
leg, when that leg is done, since a stream can carry thousands of messages:

```json
{"level":"DEBUG","msg":"fault applied","call":42,"method":"/myapp.UserService/GetUser","rule":"slow-getuser"}
{"level":"DEBUG","msg":"mutations","call":42,"direction":"response","messages":100,"rules":{"corrupt-2":21}}
```

`messages` is how many passed through that leg, `rules` how many times each rule
fired on them. A leg no rule covers stays silent.
