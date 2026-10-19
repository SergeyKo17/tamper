package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// rawBytes carries a payload through gRPC without a protobuf schema, the way the
// proxy itself does, so a test can send and receive arbitrary bytes.
type rawBytes []byte

func (r *rawBytes) Marshal() ([]byte, error) { return *r, nil }
func (r *rawBytes) Unmarshal(b []byte) error { *r = append((*r)[:0], b...); return nil }
func (r *rawBytes) ProtoMessage()            {}
func (r *rawBytes) Reset()                   {}
func (r *rawBytes) String() string           { return string(*r) }

// writeConfig puts a configuration on disk and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tamper.yml")
	rewriteConfig(t, path, content)
	return path
}

// rewriteConfig replaces the file in place, which is the change the watcher is
// there to notice.
func rewriteConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// startEcho runs a target that sends every message straight back.
func startEcho(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var msg rawBytes
		if err := stream.RecvMsg(&msg); err != nil {
			return err
		}
		return stream.SendMsg(&msg)
	}))
	t.Cleanup(srv.Stop)
	go srv.Serve(lis)

	return lis.Addr().String()
}

// configYAML is a configuration for a proxy on an ephemeral port, with whatever
// log level and rules the test needs.
func configYAML(target, level, rules string) string {
	return fmt.Sprintf("listen:\n  addr: \"127.0.0.1:0\"\ntarget:\n  addr: %q\nlogger:\n  level: %q\nrules:\n%s", target, level, rules)
}

// abortEverything answers every call with Unavailable, which is the loudest
// signal a rule can give a test.
const abortEverything = "  - match:\n" +
	"      method: \"*\"\n" +
	"    fault:\n" +
	"      type: abort\n" +
	"      code: 14\n" +
	"      prob: 1.0\n"

// start assembles the proxy from the configuration, runs it, and hands back a
// connection to it. lvl is the level variable New reports the configured level
// into, the way the process logger's own does.
func start(ctx context.Context, t *testing.T, path string, lvl *slog.LevelVar) *grpc.ClientConn {
	t.Helper()
	p, _, err := New(ctx, path, lvl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	go func() {
		if err := p.Run(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("run proxy: %v", err)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for p.Addr() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the proxy did not start listening within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	conn, err := grpc.NewClient(p.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// callCode sends one message through the proxy and reports the status code the
// client ends up with.
func callCode(t *testing.T, conn *grpc.ClientConn, body string) codes.Code {
	t.Helper()
	req := rawBytes(body)
	var resp rawBytes
	err := conn.Invoke(context.Background(), "/test/Echo", &req, &resp)
	return status.Code(err)
}

// awaitCode calls until the proxy answers with want, which is how a reload is
// observed from the outside: the file changes, and some calls later the rules
// behind it do too.
func awaitCode(t *testing.T, conn *grpc.ClientConn, want codes.Code) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got := callCode(t, conn, "hello"); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the proxy did not start answering %v within 3s", want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A configuration that is not there is a startup error: the proxy has no
// defaults of its own to fall back on.
func TestNew_MissingConfigFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, _, err := New(ctx, filepath.Join(t.TempDir(), "absent.yml"), new(slog.LevelVar)); err == nil {
		t.Fatal("expected an error for a configuration that does not exist")
	}
}

// A rule the loader rejects fails startup rather than being dropped, which
// would leave a proxy that looks healthy and injects nothing.
func TestNew_InvalidConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	path := writeConfig(t, configYAML(startEcho(t), "info",
		"  - match:\n      method: \"/test/Echo\"\n    fault:\n      type: nonsense\n      prob: 1.0\n"))
	if _, _, err := New(ctx, path, new(slog.LevelVar)); err == nil {
		t.Fatal("expected an error for an unknown fault type")
	}
}

// logger.level reaches the level the caller's handler reads, rather than being
// parsed, defaulted and then ignored.
func TestNew_AppliesLogLevel(t *testing.T) {
	for _, tc := range []struct {
		level string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"warn", slog.LevelWarn},
	} {
		t.Run(tc.level, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var lvl slog.LevelVar
			path := writeConfig(t, configYAML(startEcho(t), tc.level, ""))
			if _, _, err := New(ctx, path, &lvl); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := lvl.Level(); got != tc.want {
				t.Errorf("level = %v at level %q, want %v", got, tc.level, tc.want)
			}
		})
	}
}

// The point of the watcher: a rule added to the file starts firing on a
// connection that was already open, with no restart.
func TestNew_ReloadAppliesNewRules(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	target := startEcho(t)
	path := writeConfig(t, configYAML(target, "info", ""))
	conn := start(ctx, t, path, new(slog.LevelVar))

	if got := callCode(t, conn, "hello"); got != codes.OK {
		t.Fatalf("before the reload: code = %v, want OK", got)
	}

	rewriteConfig(t, path, configYAML(target, "info", abortEverything))
	awaitCode(t, conn, codes.Unavailable)
}

// The same path in reverse, and the one that leaves the proxy with no rules at
// all: a rule taken out of the file stops firing.
func TestNew_ReloadRemovesRules(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	target := startEcho(t)
	path := writeConfig(t, configYAML(target, "info", abortEverything))
	conn := start(ctx, t, path, new(slog.LevelVar))

	if got := callCode(t, conn, "hello"); got != codes.Unavailable {
		t.Fatalf("before the reload: code = %v, want Unavailable", got)
	}

	rewriteConfig(t, path, configYAML(target, "info", ""))
	awaitCode(t, conn, codes.OK)
}

// The level is reloaded as well, and the handler is not rebuilt for it: the
// LevelVar it already reads is what changes.
func TestNew_ReloadChangesLogLevel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var lvl slog.LevelVar
	target := startEcho(t)
	path := writeConfig(t, configYAML(target, "warn", ""))
	if _, _, err := New(ctx, path, &lvl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := lvl.Level(); got != slog.LevelWarn {
		t.Fatalf("before the reload: level = %v, want warn", got)
	}

	rewriteConfig(t, path, configYAML(target, "debug", ""))

	deadline := time.Now().Add(3 * time.Second)
	for {
		if lvl.Level() == slog.LevelDebug {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the level was not lowered to debug within 3s")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A reload that leaves the level alone leaves the logger alone: rules change
// under a level that keeps working.
func TestNew_ReloadKeepsUnchangedLogLevel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var lvl slog.LevelVar
	target := startEcho(t)
	path := writeConfig(t, configYAML(target, "debug", ""))
	conn := start(ctx, t, path, &lvl)

	rewriteConfig(t, path, configYAML(target, "debug", abortEverything))
	awaitCode(t, conn, codes.Unavailable)

	if got := lvl.Level(); got != slog.LevelDebug {
		t.Errorf("after the reload: level = %v, want debug, unchanged in the file", got)
	}
}
