package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"sync/atomic"
	"time"

	"github.com/SergeyKo17/tamper/config"
	"github.com/SergeyKo17/tamper/fault"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Proxy is a gRPC fault injection proxy.
type Proxy struct {
	server  *grpc.Server
	conn    *grpc.ClientConn
	lis     net.Listener
	injects atomic.Pointer[[]fault.Inject]
}

// New creates a Proxy that listens and forwards as described by cfg.
func New(ctx context.Context, cfg *config.Config, injects []fault.Inject) (*Proxy, error) {
	serverCreds, err := serverCredentials(cfg.Listen.TLS)
	if err != nil {
		return nil, err
	}
	tlsCfg, err := clientTLSConfig(cfg.Target.TLS)
	if err != nil {
		return nil, err
	}

	if err := checkTarget(ctx, cfg.Target.Addr, tlsCfg); err != nil {
		return nil, err
	}

	clientCreds := insecure.NewCredentials()
	if tlsCfg != nil {
		clientCreds = credentials.NewTLS(tlsCfg)
	}

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", cfg.Listen.Addr)
	if err != nil {
		return nil, fmt.Errorf("create listener: %w", err)
	}

	p := Proxy{lis: lis}
	p.injects.Store(&injects)
	p.server = grpc.NewServer(
		grpc.Creds(serverCreds),
		grpc.UnknownServiceHandler(p.handler),
		grpc.MaxRecvMsgSize(msgSize(cfg.Listen.MaxRecvMsgSize)),
		grpc.MaxSendMsgSize(msgSize(cfg.Listen.MaxSendMsgSize)),
	)

	p.conn, err = grpc.NewClient(cfg.Target.Addr,
		grpc.WithTransportCredentials(clientCreds),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(msgSize(cfg.Target.MaxRecvMsgSize)),
			grpc.MaxCallSendMsgSize(msgSize(cfg.Target.MaxSendMsgSize)),
		),
	)
	if err != nil {
		if cerr := lis.Close(); cerr != nil {
			slog.Error("listener close", "err", cerr)
		}
		return nil, fmt.Errorf("create gRPC client: %w", err)
	}
	return &p, nil
}

// msgSize turns a configured limit into a gRPC option value. Zero means the
// proxy adds no limit of its own and leaves both peers to enforce theirs.
func msgSize(limit int) int {
	if limit <= 0 {
		return math.MaxInt32
	}
	return limit
}

// Addr returns the listener's network address.
func (p *Proxy) Addr() net.Addr {
	return p.lis.Addr()
}

// SetInjects atomically replaces the active fault injectors.
func (p *Proxy) SetInjects(injects []fault.Inject) {
	p.injects.Store(&injects)
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

// forwardDesc describes every forwarded call as bidirectional streaming.
// Without a protobuf schema the proxy cannot know a method's real cardinality,
// and unary, client-streaming and server-streaming calls are all special cases
// of a bidirectional stream.
var forwardDesc = &grpc.StreamDesc{ServerStreams: true, ClientStreams: true}

// hopByHopHeader is advertised per connection, so the value received from the
// client must not be forwarded: the outgoing connection announces its own.
const hopByHopHeader = "grpc-accept-encoding"

// handler intercepts all gRPC calls, applies matching fault injectors,
// and forwards the request to the target server.
func (p *Proxy) handler(srv any, stream grpc.ServerStream) (retErr error) {
	start := time.Now()
	method, _ := grpc.Method(stream.Context())
	defer func() {
		slog.Info("request", "method", method, "duration", time.Since(start), "error", retErr)
	}()

	injects := p.injects.Load()
	for _, inj := range *injects {
		if inj.Match.Method == method {
			if err := inj.Fault.Apply(stream.Context()); err != nil {
				return err
			}
		}
	}

	md, _ := metadata.FromIncomingContext(stream.Context())
	md.Delete(hopByHopHeader)

	ctx, cancel := context.WithCancel(metadata.NewOutgoingContext(stream.Context(), md))
	defer cancel()

	clientStream, err := grpc.NewClientStream(ctx, forwardDesc, p.conn, method)
	if err != nil {
		return err
	}

	go func() {
		if err := forwardRequests(stream, clientStream); err != nil {
			slog.Debug("forward requests", "method", method, "err", err)
			cancel()
		}
	}()

	return forwardResponses(stream, clientStream)
}

// forwardRequests relays messages from the client to the target and half-closes
// the target stream once the client stops sending.
func forwardRequests(stream grpc.ServerStream, clientStream grpc.ClientStream) error {
	for {
		var msg rawBytes
		if err := stream.RecvMsg(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return clientStream.CloseSend()
			}
			return err
		}
		if err := clientStream.SendMsg(&msg); err != nil {
			// The target ended the stream. Its status is reported by
			// forwardResponses, so stop sending without failing the call.
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// forwardResponses relays headers, messages and trailers from the target back
// to the client. It returns when the call is complete.
func forwardResponses(stream grpc.ServerStream, clientStream grpc.ClientStream) error {
	// Trailers are only readable once RecvMsg has failed, so they are attached
	// on the way out. Setting empty metadata is a no-op.
	defer func() {
		stream.SetTrailer(clientStream.Trailer())
	}()

	header, err := clientStream.Header()
	if err != nil {
		return err
	}
	// Flush headers as soon as the target sends them, so its timing survives
	// the hop; SetHeader would hold them back until the first message. No
	// headers means a trailers-only reply, which stays trailers-only only as
	// long as nothing is sent.
	if header.Len() > 0 {
		if err := stream.SendHeader(header); err != nil {
			return err
		}
	}

	for {
		var msg rawBytes
		if err := clientStream.RecvMsg(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := stream.SendMsg(&msg); err != nil {
			return err
		}
	}
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
