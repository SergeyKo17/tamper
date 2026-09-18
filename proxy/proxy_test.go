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

func startEcho(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var body rawBytes
		if err := stream.RecvMsg(&body); err != nil {
			return err
		}
		return stream.SendMsg(&body)
	}))
	t.Cleanup(srv.Stop)
	go srv.Serve(lis)

	return lis.Addr().String()
}

func startProxy(t *testing.T, echoAddr string, injects []fault.Inject) *Proxy {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := &config.Config{
		Listen: config.Listen{Addr: "127.0.0.1:0"},
		Target: config.Target{Addr: echoAddr},
	}
	p, err := New(ctx, cfg, injects)
	if err != nil {
		t.Fatal(err)
	}

	go p.Run(ctx)

	return p
}

func TestProxy(t *testing.T) {
	echoAddr := startEcho(t)

	t.Run("passthrough", func(t *testing.T) {
		p := startProxy(t, echoAddr, nil)

		conn, err := grpc.NewClient(p.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		reqBody := rawBytes([]byte("hello"))
		var respBody rawBytes
		err = conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody)
		if err != nil {
			t.Fatal(err)
		}
		if string(reqBody) != string(respBody) {
			t.Errorf("expected %q, got %q", reqBody, respBody)
		}
	})

	t.Run("delay", func(t *testing.T) {
		injects := []fault.Inject{{
			Match: config.Match{Method: "/test/Echo"},
			Fault: fault.NewDelay(100*time.Millisecond, 1.0),
		}}
		p := startProxy(t, echoAddr, injects)

		conn, err := grpc.NewClient(p.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		start := time.Now()
		reqBody := rawBytes([]byte("hello"))
		var respBody rawBytes
		err = conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody)
		if err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		if elapsed < 100*time.Millisecond {
			t.Errorf("expected delay >= 100ms, got %v", elapsed)
		}
		if string(reqBody) != string(respBody) {
			t.Errorf("expected %q, got %q", reqBody, respBody)
		}
	})

	t.Run("abort", func(t *testing.T) {
		injects := []fault.Inject{{
			Match: config.Match{Method: "/test/Echo"},
			Fault: fault.NewAbort(int(codes.Internal), "injected", 1.0),
		}}
		p := startProxy(t, echoAddr, injects)

		conn, err := grpc.NewClient(p.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		reqBody := rawBytes([]byte("hello"))
		var respBody rawBytes
		err = conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok {
			t.Fatalf("expected gRPC status error, got %v", err)
		}
		if st.Code() != codes.Internal {
			t.Errorf("expected code Internal, got %v", st.Code())
		}
		if st.Message() != "injected" {
			t.Errorf("expected message %q, got %q", "injected", st.Message())
		}
	})
}
