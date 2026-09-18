package proxy

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SergeyKo17/tamper/config"
	"github.com/SergeyKo17/tamper/fault"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
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

func startProxy(t *testing.T, echoAddr string, injects []fault.Inject) *Proxy {
	t.Helper()
	return startProxyWithConfig(t, &config.Config{
		Listen: config.Listen{Addr: "127.0.0.1:0"},
		Target: config.Target{Addr: echoAddr},
	}, injects)
}

func startProxyWithConfig(t *testing.T, cfg *config.Config, injects []fault.Inject) *Proxy {
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
		Match: config.Match{Method: "/test/Echo"},
		Fault: fault.NewDelay(100*time.Millisecond, 1.0),
	}}
	p := startProxy(t, startEcho(t), injects)
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
		Match: config.Match{Method: "/test/Echo"},
		Fault: fault.NewAbort(int(codes.Internal), "injected", 1.0),
	}}
	p := startProxy(t, startEcho(t), injects)
	conn := dialProxy(t, p)

	reqBody := rawBytes([]byte("hello"))
	var respBody rawBytes
	err := conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertStatus(t, err, codes.Internal, "injected")
}

func TestProxy_SetInjects(t *testing.T) {
	p := startProxy(t, startEcho(t), nil)
	conn := dialProxy(t, p)

	reqBody := rawBytes([]byte("hello"))
	var respBody rawBytes
	if err := conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody); err != nil {
		t.Fatal(err)
	}

	p.SetInjects([]fault.Inject{{
		Match: config.Match{Method: "/test/Echo"},
		Fault: fault.NewAbort(int(codes.Internal), "dynamic", 1.0),
	}})

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
