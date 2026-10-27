package proxy

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/SergeyKo17/tamper/config"
	"github.com/SergeyKo17/tamper/fault"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// echoHandler sends one received message straight back.
func echoHandler(_ any, stream grpc.ServerStream) error {
	var body rawBytes
	if err := stream.RecvMsg(&body); err != nil {
		return err
	}
	return stream.SendMsg(&body)
}

// startHang serves a call that never answers, so a stream through it stays
// open. called is closed when the first call arrives.
func startHang(t *testing.T) (addr string, called chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	called = make(chan struct{})
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, _ grpc.ServerStream) error {
		close(called)
		<-ctx.Done()
		return nil
	}))
	t.Cleanup(srv.Stop)
	go srv.Serve(lis)
	return lis.Addr().String(), called
}

func startEcho(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := grpc.NewServer(grpc.UnknownServiceHandler(echoHandler))
	t.Cleanup(srv.Stop)
	go srv.Serve(lis)

	return lis.Addr().String()
}

func startProxy(t *testing.T, echoAddr string, injects *fault.Injects) *Proxy {
	t.Helper()
	return startProxyWithConfig(t, &config.Config{
		Listen: config.Listen{Addr: "127.0.0.1:0"},
		Target: config.Target{Addr: echoAddr},
	}, injects)
}

func startProxyWithConfig(t *testing.T, cfg *config.Config, injects *fault.Injects) *Proxy {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p, err := New(ctx, cfg, injects)
	if err != nil {
		t.Fatal(err)
	}

	go p.Run(ctx)

	return p
}

func dialProxy(t *testing.T, p *Proxy) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(p.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestProxy_Passthrough(t *testing.T) {
	p := startProxy(t, startEcho(t), nil)
	conn := dialProxy(t, p)

	reqBody := rawBytes([]byte("hello"))
	var respBody rawBytes
	if err := conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody); err != nil {
		t.Fatal(err)
	}
	if string(reqBody) != string(respBody) {
		t.Errorf("expected %q, got %q", reqBody, respBody)
	}
}

func TestProxy_Delay(t *testing.T) {
	injects := []fault.Inject{{
		Match:       config.Match{Method: "/test/Echo"},
		Probability: 1.0,
		Fault:       fault.NewDelay(100 * time.Millisecond),
	}}
	p := startProxy(t, startEcho(t), &fault.Injects{Call: injects})
	conn := dialProxy(t, p)

	start := time.Now()
	reqBody := rawBytes([]byte("hello"))
	var respBody rawBytes
	if err := conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("expected delay >= 100ms, got %v", elapsed)
	}
	if string(reqBody) != string(respBody) {
		t.Errorf("expected %q, got %q", reqBody, respBody)
	}
}

func TestProxy_Abort(t *testing.T) {
	injects := []fault.Inject{{
		Match:       config.Match{Method: "/test/Echo"},
		Probability: 1.0,
		Fault:       fault.NewAbort(int(codes.Internal), "injected"),
	}}
	p := startProxy(t, startEcho(t), &fault.Injects{Call: injects})
	conn := dialProxy(t, p)

	reqBody := rawBytes([]byte("hello"))
	var respBody rawBytes
	err := conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertStatus(t, err, codes.Internal, "injected")
}

// TestProxy_WildcardRule checks that a pattern, not just an exact method name,
// selects the calls a rule applies to.
func TestProxy_WildcardRule(t *testing.T) {
	injects := []fault.Inject{{
		Match:       config.Match{Method: "/test/*"},
		Probability: 1.0,
		Fault:       fault.NewAbort(int(codes.Unavailable), "wildcard"),
	}}
	p := startProxy(t, startEcho(t), &fault.Injects{Call: injects})
	conn := dialProxy(t, p)

	reqBody := rawBytes([]byte("hello"))
	var respBody rawBytes
	err := conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody)
	if err == nil {
		t.Fatal("expected the wildcard rule to fire, got nil")
	}
	assertStatus(t, err, codes.Unavailable, "wildcard")
}

func TestProxy_SetInjects(t *testing.T) {
	p := startProxy(t, startEcho(t), nil)
	conn := dialProxy(t, p)

	reqBody := rawBytes([]byte("hello"))
	var respBody rawBytes
	if err := conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody); err != nil {
		t.Fatal(err)
	}

	p.SetInjects(&fault.Injects{Call: []fault.Inject{{
		Match:       config.Match{Method: "/test/Echo"},
		Probability: 1.0,
		Fault:       fault.NewAbort(int(codes.Internal), "dynamic"),
	}}})

	err := conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody)
	if err == nil {
		t.Fatal("expected error after SetInjects, got nil")
	}
	assertStatus(t, err, codes.Internal, "dynamic")
}

func assertStatus(t *testing.T, err error, want codes.Code, msg string) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != want {
		t.Errorf("expected code %v, got %v", want, st.Code())
	}
	if st.Message() != msg {
		t.Errorf("expected message %q, got %q", msg, st.Message())
	}
}

// addMeta is an injector that only mutates the context: it appends one outgoing
// header and lets the call through. Its effect is invisible unless the handler
// carries the returned context forward.
type addMeta struct {
	key   string
	value string
}

func (a addMeta) Apply(ctx context.Context) (context.Context, error) {
	return metadata.AppendToOutgoingContext(ctx, a.key, a.value), nil
}

// TestProxy_ChainThreadsContext checks that the injectors form a chain: each
// one receives what the previous returned, and all of it reaches the target.
// Two rules match the same method, so a handler that restarts from the incoming
// context on every iteration keeps only the last header. The client's own
// metadata is checked alongside, since a chain built before the outgoing
// context would drop it.
func TestProxy_ChainThreadsContext(t *testing.T) {
	injects := []fault.Inject{
		{Match: config.Match{Method: "/test/Echo"}, Probability: 1.0,
			Fault: addMeta{key: "x-first", value: "1"}},
		{Match: config.Match{Method: "/test/Echo"}, Probability: 1.0,
			Fault: addMeta{key: "x-second", value: "2"}},
	}
	p := startProxy(t, startMetaEcho(t, nil), &fault.Injects{Call: injects})
	conn := dialProxy(t, p)

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "bearer test")

	cases := []struct {
		key  string
		want string
	}{
		{key: "x-first", want: "1"},
		{key: "x-second", want: "2"},
		{key: "authorization", want: "bearer test"},
	}
	for _, c := range cases {
		if got := askTarget(t, conn, ctx, c.key); got != c.want {
			t.Errorf("target saw %s = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestProxy_RunStopsOnHungCall(t *testing.T) {
	logs := captureLogs(t)
	addr, called := startHang(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := New(ctx, &config.Config{
		Listen: config.Listen{Addr: "127.0.0.1:0"},
		Target: config.Target{Addr: addr},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.stopTimeout = 50 * time.Millisecond

	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	conn := dialProxy(t, p)
	go func() {
		body := rawBytes([]byte("hello"))
		var resp rawBytes
		_ = conn.Invoke(context.Background(), "/test/Echo", &body, &resp)
	}()

	// The call reached the target and never comes back, so a graceful stop
	// cannot finish on its own.
	<-called
	stopped := time.Now()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the stop timeout")
	}

	if elapsed := time.Since(stopped); elapsed < p.stopTimeout {
		t.Errorf("Run returned in %v, before the %v timeout", elapsed, p.stopTimeout)
	}
	if out := logs.String(); !strings.Contains(out, "graceful stop timed out") {
		t.Errorf("no timeout in the log: %s", out)
	}
}
