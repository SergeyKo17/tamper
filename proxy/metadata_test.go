package proxy

import (
	"context"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	echoHeaderKey    = "x-echo-header"
	echoHeaderValue  = "from-target-header"
	echoTrailerKey   = "x-echo-trailer"
	echoTrailerValue = "from-target-trailer"
)

// startMetaEcho runs a target that reports the request metadata it received: the
// request body names a metadata key, and the response body carries its value.
// Every reply also carries a fixed header and trailer.
func startMetaEcho(t *testing.T, fail error) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		stream.SetTrailer(metadata.Pairs(echoTrailerKey, echoTrailerValue))
		if fail != nil {
			return fail
		}
		if err := stream.SetHeader(metadata.Pairs(echoHeaderKey, echoHeaderValue)); err != nil {
			return err
		}

		var key rawBytes
		if err := stream.RecvMsg(&key); err != nil {
			return err
		}
		md, _ := metadata.FromIncomingContext(stream.Context())
		seen := rawBytes(strings.Join(md.Get(string(key)), ","))
		return stream.SendMsg(&seen)
	}))
	t.Cleanup(srv.Stop)
	go srv.Serve(lis)

	return lis.Addr().String()
}

// askTarget asks the target which value it saw for the given metadata key.
func askTarget(t *testing.T, conn *grpc.ClientConn, ctx context.Context, key string, opts ...grpc.CallOption) string {
	t.Helper()
	req := rawBytes(key)
	var resp rawBytes
	if err := conn.Invoke(ctx, "/test/Echo", &req, &resp, opts...); err != nil {
		t.Fatal(err)
	}
	return string(resp)
}

func TestProxy_ForwardsRequestMetadata(t *testing.T) {
	p := startProxy(t, startMetaEcho(t, nil), nil)
	conn := dialProxy(t, p)

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "bearer test")
	if got := askTarget(t, conn, ctx, "authorization"); got != "bearer test" {
		t.Errorf("target saw authorization = %q, want %q", got, "bearer test")
	}
}

func TestProxy_StripsHopByHopHeader(t *testing.T) {
	p := startProxy(t, startMetaEcho(t, nil), nil)
	conn := dialProxy(t, p)

	// The client advertises a codec the proxy's own connection knows nothing
	// about; forwarding it would speak for a hop that cannot honour it.
	ctx := metadata.AppendToOutgoingContext(context.Background(), hopByHopHeader, "bogus-codec")
	if got := askTarget(t, conn, ctx, hopByHopHeader); strings.Contains(got, "bogus-codec") {
		t.Errorf("target saw the client's %s = %q", hopByHopHeader, got)
	}
}

func TestProxy_ForwardsResponseHeaderAndTrailer(t *testing.T) {
	p := startProxy(t, startMetaEcho(t, nil), nil)
	conn := dialProxy(t, p)

	var header, trailer metadata.MD
	askTarget(t, conn, context.Background(), "authorization",
		grpc.Header(&header), grpc.Trailer(&trailer))

	if got := header.Get(echoHeaderKey); len(got) != 1 || got[0] != echoHeaderValue {
		t.Errorf("header %s = %v, want [%s]", echoHeaderKey, got, echoHeaderValue)
	}
	if got := trailer.Get(echoTrailerKey); len(got) != 1 || got[0] != echoTrailerValue {
		t.Errorf("trailer %s = %v, want [%s]", echoTrailerKey, got, echoTrailerValue)
	}
}

func TestProxy_ForwardsTrailerOnError(t *testing.T) {
	targetErr := status.Error(codes.ResourceExhausted, "quota")
	p := startProxy(t, startMetaEcho(t, targetErr), nil)
	conn := dialProxy(t, p)

	var trailer metadata.MD
	req := rawBytes("authorization")
	var resp rawBytes
	err := conn.Invoke(context.Background(), "/test/Echo", &req, &resp, grpc.Trailer(&trailer))
	if err == nil {
		t.Fatal("expected error from target, got nil")
	}
	assertStatus(t, err, codes.ResourceExhausted, "quota")

	if got := trailer.Get(echoTrailerKey); len(got) != 1 || got[0] != echoTrailerValue {
		t.Errorf("trailer %s = %v, want [%s]", echoTrailerKey, got, echoTrailerValue)
	}
}
