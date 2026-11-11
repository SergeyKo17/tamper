# Demo

A two-container stack that shows a fault being injected and a rule being
changed without a restart: a small gRPC echo server, and tamper in front of it.

## What is here

| Path | What it is |
|------|------------|
| `echo/` | The mock: one unary method, `/demo.Echo/Say`, with reflection on |
| `compose.yml` | Both services, ports bound to `127.0.0.1` only |
| `config/demo.yml` | The proxy config, mounted read-only into the container |
| `demo.ps1`, `run.cmd` | The driver that types and runs the recorded session |
| `demo.gif` | The recording used in the top-level README |

## Run it

```bash
docker compose up -d
```

The mock answers on `127.0.0.1:50051`, tamper on `127.0.0.1:9090`. Both are
built from the repository root, so no image needs to be pulled.

Call the mock directly:

```bash
grpcurl -plaintext -d '{"text":"hello"}' 127.0.0.1:50051 demo.Echo/Say
```

PowerShell 5.1 needs it written differently: a bare `@` is a token its parser
rejects, so the flag is quoted, and the body goes in over stdin, where nothing
strips the quotes out of the JSON.

```powershell
'{"text":"hello"}' | grpcurl -plaintext -d '@' 127.0.0.1:50051 demo.Echo/Say
```

The calls below differ only in the port.

Now the same call through tamper. `config/demo.yml` holds one rule on
`/demo.Echo/Say` with `prob: 1.0`, so the fault always fires:

```bash
grpcurl -plaintext -d '{"text":"hello"}' 127.0.0.1:9090 demo.Echo/Say
```

The answer is the same, two seconds late.

## Change the rule without a restart

Edit `config/demo.yml` and change `type: delay` to `type: abort`. The
`duration` and the `code` both sit in the rule already, and the field
belonging to the other type is ignored. Then:

```bash
docker compose kill -s HUP tamper
```

`kill` only sends the signal here; tamper handles it, rereads the file and
swaps its rules. The container keeps running. The next call fails with
`Unavailable: injected by tamper`, and the mock never sees it.

A bind mount from a Windows host delivers no file system events to the
container, which is why the demo reloads on `SIGHUP` rather than on a watch.

The config is mounted as a directory, not as a single file: an editor that
saves by rename would be invisible behind a single-file mount.

Faults that fired are logged at debug level, which `config/demo.yml` turns on:

```bash
docker compose logs tamper | grep "fault applied"
```

```powershell
docker compose logs tamper | Select-String "fault applied"
```

## Re-recording the gif

`demo.ps1` prints each command as if typed, runs it, and holds the frame.
`run.cmd` exists only so the recording starts with `.\run` instead of a line
full of PowerShell flags.

1. Run `.\run`. It recreates the stack and warms the proxy's first connection
   off camera, then waits.
2. Start a recorder on the terminal window — [ScreenToGif][stg] at 10–12 fps.
3. Press Enter. The screen clears and the session plays for about 30 seconds.
4. Save as `demo.gif`.

The script restores `config/demo.yml` when it finishes, so the tree stays
clean whichever rule the recording ended on.

[stg]: https://www.screentogif.com/

## Regenerating the stubs

`echo/echopb` is committed. After editing `echo/echo.proto`, run from the
repository root:

```bash
make demo-proto
```

The mock lives in the root module on purpose: a nested module would fall out
of `./...` and out of CI.