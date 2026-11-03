package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"sync"
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

// defaultStopTimeout is the deadline for everything the calls have not let go
// of once the proxy is stopping: a connection the client has not closed, or a
// response it stopped reading. Cancelling a call reaches its target side, not
// the client side, so past this point the transport is torn down instead.
const defaultStopTimeout = 5 * time.Second

// handshakeTimeout bounds how long a new connection may take to finish the
// HTTP/2 handshake. Both the graceful and the forced stop wait for connections
// still in it, so a peer that opens a socket and sends nothing would hold the
// shutdown well past the stop timeout.
const handshakeTimeout = 5 * time.Second

// errShuttingDown is the cause a call's context carries when the shutdown, and
// not the client or an injected fault, ended it.
var errShuttingDown = errors.New("shutting down")

// Proxy is a gRPC fault injection proxy.
type Proxy struct {
	server  *grpc.Server
	conn    *grpc.ClientConn
	lis     net.Listener
	injects atomic.Pointer[fault.Injects]
	// calls numbers the calls passing through, so the lines a single call
	// leaves behind can be told apart from those of the calls beside it.
	calls atomic.Uint64
	// stopTimeout caps the graceful stop; see defaultStopTimeout.
	stopTimeout time.Duration
	// shutdown is closed when the proxy stops, to cancel the calls in flight.
	shutdown       context.Context
	shutdownCancel context.CancelFunc
	// closeOnce keeps Close idempotent: its owner defers it right after New,
	// whether the proxy went on to run or not.
	closeOnce sync.Once
	closeErr  error
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

	shutdown, shutdownCancel := context.WithCancel(context.Background())
	p := Proxy{lis: lis, stopTimeout: defaultStopTimeout, shutdown: shutdown, shutdownCancel: shutdownCancel}
	p.storeInjects(injects)
	p.server = grpc.NewServer(
		grpc.Creds(serverCreds),
		grpc.UnknownServiceHandler(p.handler),
		grpc.MaxRecvMsgSize(msgSize(cfg.Listen.MaxRecvMsgSize)),
		grpc.MaxSendMsgSize(msgSize(cfg.Listen.MaxSendMsgSize)),
		grpc.ConnectionTimeout(handshakeTimeout),
	)

	p.conn, err = grpc.NewClient(cfg.Target.Addr,
		grpc.WithTransportCredentials(clientCreds),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(msgSize(cfg.Target.MaxRecvMsgSize)),
			grpc.MaxCallSendMsgSize(msgSize(cfg.Target.MaxSendMsgSize)),
		),
	)
	if err != nil {
		shutdownCancel()
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

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	defer context.AfterFunc(p.shutdown, func() { cancel(errShuttingDown) })()
	defer func() {
		if errors.Is(context.Cause(ctx), errShuttingDown) && isCanceled(retErr) {
			retErr = status.Error(codes.Unavailable, "proxy is shutting down")
		}
	}()

	var err error
	for _, inj := range injects.Call {
		if !inj.Match.Matches(method) || !inj.Fires() {
			continue
		}
		log.Debug("fault applied", "method", method, "rule", inj.Name)
		if ctx, err = inj.Fault.Apply(ctx); err != nil {
			return err
		}
	}

	clientStream, err := grpc.NewClientStream(ctx, forwardDesc, p.conn, method)
	if err != nil {
		return err
	}

	reqRules, respRules := selectMutators(injects.Message, method)

	// A failure on the request side tears the call down through cancel(), which
	// reaches the response side as a bare Canceled. The real cause is kept here
	// so the client is told what actually went wrong.
	var reqErr atomic.Pointer[error]
	// This outlives the handler whenever the response side ends first: a target
	// that answers without reading the request to the end, or the shutdown
	// cancelling the call. It is then left in a RecvMsg on a stream grpc-go has
	// already finished, which ends it with an error. Nothing waits for it,
	// because waiting is the hang the shutdown exists to avoid.
	go func() {
		if err := forwardRequests(stream, clientStream, reqRules, log); err != nil {
			log.Warn("forward requests", "method", method, "err", err)
			reqErr.Store(&err)
			cancel(err)
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
		if !r.Fires() {
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

// Run serves until ctx is canceled or the server fails on its own, stops both
// ways round, and returns once everything is closed.
func (p *Proxy) Run(ctx context.Context) error {
	served := make(chan error, 1)
	go func() { served <- p.server.Serve(p.lis) }()

	select {
	case err := <-served:
		// The listener is gone, while the calls it accepted are not: stopping
		// is what ends them.
		p.stop("serve failed")
		return err
	case <-ctx.Done():
	}

	p.stop("context canceled")
	// Serve returns once the stop has run its course. ErrServerStopped is the
	// stop we asked for; anything else went wrong on the way to it.
	if err := <-served; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}
	return nil
}

// stop ends the server, giving the calls in flight stopTimeout to let go of
// what they hold. The reason is logged because the lines that follow say how
// the stop went, not what started it.
func (p *Proxy) stop(reason string) {
	slog.Info("stopping", "reason", reason)
	p.shutdownCancel()
	stopped := time.Now()

	done := make(chan struct{})
	go func() {
		p.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("graceful stop", "duration", time.Since(stopped).String())
	case <-time.After(p.stopTimeout):
		slog.Warn("graceful stop timed out", "timeout", p.stopTimeout.String())
		p.server.Stop()
		<-done
	}
}

// Close releases what New took: the connection to the target and the listener.
// Running the proxy does not close them, so this is the owner's to call, and it
// is safe to call more than once.
func (p *Proxy) Close() error {
	p.closeOnce.Do(func() {
		p.shutdownCancel()
		p.closeErr = p.conn.Close()
		// On the usual path the server closed the listener while stopping.
		if err := p.lis.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			p.closeErr = errors.Join(p.closeErr, err)
		}
	})
	return p.closeErr
}

// isCanceled reports whether err carries a cancellation. The forms differ by
// origin: the streams fail with a gRPC status, while a fault interrupted
// mid-delay returns the bare context error.
func isCanceled(err error) bool {
	return status.Code(err) == codes.Canceled || errors.Is(err, context.Canceled)
}
