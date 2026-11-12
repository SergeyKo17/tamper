package proxy

import (
	"bytes"
	"context"
	"testing"

	"google.golang.org/grpc"
)

// TestProxy_GzipRoundTrip sends a compressed call through the proxy. Each hop
// negotiates compression on its own, so the proxy needs a gzip compressor of
// its own: without one the listener cannot unpack the request and the call
// fails with Unimplemented before it ever reaches the target.
//
// The compressor is named by literal rather than by gzip.Name on purpose:
// importing the gzip package here would register it for the test binary and
// hide the loss of the blank import in proxy.go.
func TestProxy_GzipRoundTrip(t *testing.T) {
	p := startProxy(t, startEcho(t), nil)
	conn := dialProxy(t, p)

	// Well past gRPC's 1 KiB buffer pooling threshold, so the payload travels
	// the pooled path on both hops.
	reqBody := rawBytes(bytes.Repeat([]byte("payload"), 1024))
	var respBody rawBytes
	if err := conn.Invoke(context.Background(), "/test/Echo", &reqBody, &respBody,
		grpc.UseCompressor("gzip")); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(reqBody, respBody) {
		t.Errorf("response differs from request: sent %d bytes, got %d back",
			len(reqBody), len(respBody))
	}
}
