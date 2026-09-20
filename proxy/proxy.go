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

	_ "google.golang.org/grpc/encoding/gzip"

	"github.com/SergeyKo17/tamper/config"
	"github.com/SergeyKo17/tamper/fault"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Proxy is a gRPC fault injection proxy.
type Proxy struct {
	server  *grpc.Server
	conn    *grpc.ClientConn
	lis     net.Listener
	injects atomic.Pointer[fault.Injects]
	// calls numbers the calls passing through, so the lines a single call
	// leaves behind can be told apart from those of the calls beside it.
	calls atomic.Uint64
}

// New creates a Proxy that listens and forwards as described by cfg.
func New(ctx context.Context, cfg *config.Config, injects *fault.Injects) (*Proxy, error) {
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
	p.storeInjects(injects)
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
func (p *Proxy) SetInjects(injects *fault.Injects) {
	p.storeInjects(injects)
}

// storeInjects keeps the stored pointer non-nil: no rules means empty lists,
// not a nil that every call would dereference.
func (p *Proxy) storeInjects(injects *fault.Injects) {
	if injects == nil {
		injects = &fault.Injects{}
	}
	p.injects.Store(injects)
}

// rawBytes implements proto.Message and grpc encoding interfaces to pass
// gRPC payloads through without knowing the protobuf schema.
type rawBytes []byte

// Marshal returns the raw bytes as-is, skipping protobuf serialization.
func (r *rawBytes) Marshal() ([]byte, error) { return *r, nil }

// Unmarshal copies incoming bytes without protobuf deserialization. The copy is
// required: the buffer belongs to a pool that gRPC reclaims as soon as the codec
// returns, so keeping a reference to it would hand us memory someone else owns.
func (r *rawBytes) Unmarshal(b []byte) error { *r = append((*r)[:0], b...); return nil }

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
	log := slog.With("call", p.calls.Add(1))
	defer func() {
		log.Info("request", "method", method, "duration", time.Since(start).String(), "error", retErr)
	}()

	md, _ := metadata.FromIncomingContext(stream.Context())
	md.Delete(hopByHopHeader)

	ctx := metadata.NewOutgoingContext(stream.Context(), md)

	injects := p.injects.Load()

	var err error
	for _, inj := range injects.Call {
		if !inj.Match.Matches(method) || !fault.Fires(inj.Probability) {
			continue
		}
		log.Debug("fault applied", "method", method, "rule", inj.Name)
		if ctx, err = inj.Fault.Apply(ctx); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	clientStream, err := grpc.NewClientStream(ctx, forwardDesc, p.conn, method)
	if err != nil {
		return err
	}

	reqRules, respRules := selectMutators(injects.Message, method)

	// A failure on the request side tears the call down through cancel(), which
	// reaches the response side as a bare Canceled. The real cause is kept here
	// so the client is told what actually went wrong.
	var reqErr atomic.Pointer[error]
	go func() {
		if err := forwardRequests(stream, clientStream, reqRules, log); err != nil {
			log.Warn("forward requests", "method", method, "err", err)
			reqErr.Store(&err)
			cancel()
		}
	}()

	err = forwardResponses(stream, clientStream, respRules, log)
	if err != nil && status.Code(err) == codes.Canceled {
		// Stored before cancel(), and Canceled can only be observed after it.
		if cause := reqErr.Load(); cause != nil {
			return *cause
		}
	}
	return err
}

// forwardRequests relays messages from the client to the target and half-closes
// the target stream once the client stops sending.
func forwardRequests(stream grpc.ServerStream, clientStream grpc.ClientStream, rules []fault.MessageInject, log *slog.Logger) error {
	stats := newMutationStats(rules)
	defer stats.log(log, config.DirectionRequest)

	for {
		var msg rawBytes
		if err := stream.RecvMsg(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return clientStream.CloseSend()
			}
			return err
		}

		out, forward, err := applyMutators(msg, rules, &stats)
		if err != nil {
			return err
		}
		if !forward {
			continue
		}
		msg = out

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
func forwardResponses(stream grpc.ServerStream, clientStream grpc.ClientStream, rules []fault.MessageInject, log *slog.Logger) error {
	stats := newMutationStats(rules)
	defer stats.log(log, config.DirectionResponse)

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

		out, forward, err := applyMutators(msg, rules, &stats)
		if err != nil {
			return err
		}
		if !forward {
			continue
		}
		msg = out

		if err := stream.SendMsg(&msg); err != nil {
			return err
		}
	}
}

// selectMutators picks the mutators a method falls under and splits them by the
// leg they work on. Matching happens once per call rather than once per message:
// the method cannot change mid-call, and a stream may carry thousands of them.
func selectMutators(injects []fault.MessageInject, method string) (req, resp []fault.MessageInject) {
	for _, in := range injects {
		if !in.Match.Matches(method) {
			continue
		}
		if in.Direction != config.DirectionResponse {
			req = append(req, in)
		}
		if in.Direction != config.DirectionRequest {
			resp = append(resp, in)
		}
	}
	return req, resp
}

// mutationStats counts what a leg did, so one line can report it at the end of
// the call instead of one line per message. It belongs to a single pump and is
// touched by that pump alone, which is why nothing here is atomic.
type mutationStats struct {
	// active tells an untouched leg from one that carried no faulty message,
	// so a call nobody configured a rule for stays silent in the log.
	active   bool
	messages int
	fired    map[string]int
}

func newMutationStats(rules []fault.MessageInject) mutationStats {
	return mutationStats{active: len(rules) > 0, fired: make(map[string]int, len(rules))}
}

func (s *mutationStats) log(log *slog.Logger, direction string) {
	if !s.active {
		return
	}
	log.Debug("mutations", "direction", direction, "messages", s.messages, "rules", s.fired)
}

// applyMutators runs one message through the chain, in configuration order. A
// mutator that refuses to forward ends the chain: a message that will not be
// sent has nothing left to change.
func applyMutators(msg []byte, rules []fault.MessageInject, stats *mutationStats) ([]byte, bool, error) {
	stats.messages++
	for _, r := range rules {
		if !fault.Fires(r.Probability) {
			continue
		}
		out, forward, err := r.Mutator.Mutate(msg)
		if err != nil {
			return nil, false, err
		}
		stats.fired[r.Name]++
		if !forward {
			return nil, false, nil
		}
		msg = out
	}
	return msg, true, nil
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
