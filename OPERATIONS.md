# Operations

What tamper does while it runs: reread its configuration, and stop.

## Reloading the configuration

There are two ways in, and both end up rereading the same file:

- **The file changes.** tamper watches the directory the config lives in and
  reloads a moment after a write settles. This is what you want while editing
  rules locally.
- **SIGHUP.** `kill -HUP $(pidof tamper)` rereads the file on demand, whether or
  not the file system reported anything.

A reload swaps the rules and the log level on connections that are already open.
A file that does not parse is logged and otherwise ignored: the rules in force
stay in force, because a typo should not quietly leave the proxy injecting
nothing.

A changed `listen` or `target` is not applied: those are read once, at startup.
The reload says so, naming the fields it left alone, so the line in the log is
not just `configuration reloaded`.

### In a container

The signal is not a convenience there, it is the only way:

```bash
docker kill -s HUP tamper
```

A config bind-mounted from a Windows host reaches the container through a
translation layer that delivers no file system events, so the watcher never
learns of the change. Mounting the directory rather than the file alone is still
worth doing — a single-file mount hides the rename an editor saves through — but
it does not help here, and the signal is what reloads the proxy.

## Stopping

SIGINT and SIGTERM stop the proxy. New connections are refused at once, and the
calls in flight are ended rather than waited for. Each client is told why:

```
rpc error: code = Unavailable desc = proxy is shutting down
```

Waiting them out is not an option: tamper relays every call as a stream, so a
target that stops answering leaves behind a call that would hold the process
open forever. Ending them deliberately also keeps the shutdown
apart from an injected fault, which a torn connection would be
indistinguishable from.

What is left to wait for is whatever the calls have not let go of: a client that
has not closed its connection once it saw the GOAWAY, or one that stopped
reading a response. Cancelling a call reaches its target side, not the client
side. Usually all of it costs a round trip:

```json
{"level":"INFO","msg":"stopping","reason":"context canceled"}
{"level":"INFO","msg":"graceful stop","duration":"1.2ms"}
```

The first line says what started the stop. It matters when nothing asked for
one: should the listener fail on its own, the reason reads `serve failed`, and
the lines that follow are about an already broken proxy rather than about this
one.

Five seconds is the limit, and past it the transport is torn down — the only
thing that unblocks a write to a client that is not reading:

```json
{"level":"WARN","msg":"graceful stop timed out","timeout":"5s"}
```

In a container the cap is what makes the difference. `docker stop` sends
SIGTERM and follows it with SIGKILL ten seconds later; five seconds leaves the
forced stop room to finish before the process is killed outright.
