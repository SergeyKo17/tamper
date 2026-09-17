package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/SergeyKo17/tamper/fault"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Proxy is a gRPC fault injection proxy.
type Proxy struct {
	server  *grpc.Server
	conn    *grpc.ClientConn
	lis     net.Listener
	injects []fault.Inject
}

// New creates a Proxy that listens on listenAddr and forwards to targetAddr.
func New(ctx context.Context, listenAddr, targetAddr string, injects []fault.Inject) (*Proxy, error) {
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("create listener: %w", err)
	}

	p := Proxy{
		lis:     lis,
		injects: injects,
	}
	p.server = grpc.NewServer(grpc.UnknownServiceHandler(p.handler))

	p.conn, err = grpc.NewClient(targetAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("create gRPC client: %w", err)
	}
	return &p, nil
}

// Addr returns the listener's network address.
func (p *Proxy) Addr() net.Addr {
	return p.lis.Addr()
}

// rawBytes implements proto.Message and grpc encoding interfaces to pass
// gRPC payloads through without knowing the protobuf schema.
type rawBytes []byte

// Marshal returns the raw bytes as-is, skipping protobuf serialization.
func (r *rawBytes) Marshal() ([]byte, error) { return *r, nil }

// Unmarshal stores incoming bytes without protobuf deserialization.
func (r *rawBytes) Unmarshal(b []byte) error { *r = b; return nil }

// ProtoMessage marks rawBytes as a valid proto.Message implementation.
func (r *rawBytes) ProtoMessage() {}

// Reset is a no-op required by the proto.Message interface.
func (r *rawBytes) Reset() {}

// String returns the raw bytes as a string for debugging.
func (r *rawBytes) String() string { return string(*r) }

// handler intercepts all gRPC calls, applies matching fault injectors,
// and forwards the request to the target server.
func (p *Proxy) handler(srv any, stream grpc.ServerStream) (retErr error) {
	start := time.Now()
	method, _ := grpc.Method(stream.Context())
	defer func() {
		slog.Info("request", "method", method, "duration", time.Since(start), "error", retErr)
	}()
	for _, inj := range p.injects {
		if inj.Match.Method == method {
			err := inj.Fault.Apply(stream.Context())
			if err != nil {
				return err
			}
		}
	}

	var reqBody rawBytes
	if err := stream.RecvMsg(&reqBody); err != nil {
		return err
	}

	var respBody rawBytes
	err := p.conn.Invoke(stream.Context(), method, &reqBody, &respBody)
	if err != nil {
		return err
	}
	return stream.SendMsg(&respBody)
}

// Run starts the proxy server and blocks until the context is canceled.
func (p *Proxy) Run(ctx context.Context) error {
	go func() {
		defer func() {
			if err := p.conn.Close(); err != nil {
				slog.Error("client connection close", "err", err)
			}
		}()
		<-ctx.Done()
		p.server.GracefulStop()
	}()

	return p.server.Serve(p.lis)
}
