# Faults

A rule pairs a `match`, which selects calls by method, with a `fault`, which is
what happens to them. Rules live under `rules` in the config file; everything
else about the file is in [CONFIGURATION.md](CONFIGURATION.md).

## Rules

| Field | Description |
|-------|-------------|
| `match.method` | gRPC method path, exact or with `*` wildcards, e.g. `/package.Service/Method` |
| `fault.name` | Name the rule is reported under in the logs; defaults to its type and position, e.g. `corrupt-2` |
| `fault.type` | `delay`, `abort`, `truncate`, `corrupt` or `drop` |
| `fault.prob` | Probability 0.0–1.0 that the fault fires |
| `fault.duration` | Delay duration, e.g. `200ms`, `1s` (delay only) |
| `fault.code` | gRPC status code 1–16 (abort only) |
| `fault.msg` | Error message returned to client (abort only) |
| `fault.direction` | `request`, `response` or `both` (message faults only, default `request`) |
| `fault.size` | Bytes to keep (truncate only) |
| `fault.count` | Bytes to damage, capped at the message length (corrupt only, default `1`) |

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

## Message faults

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

**`corrupt`** damages `count` bytes drawn at random, leaving the length alone.
No two of them are the same byte, so `count` is what you get rather than an
upper bound, and a `count` past the end of the message damages all of it.
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
