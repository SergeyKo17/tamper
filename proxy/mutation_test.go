package proxy

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SergeyKo17/tamper/config"
	"github.com/SergeyKo17/tamper/fault"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// startReportEcho runs a target that answers "<length>:<body>". The length is
// what makes the two legs tell each other apart: it reports the request as the
// target saw it, inside a response the proxy can still damage on the way back.
func startReportEcho(t *testing.T) string {
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
		reply := rawBytes(strconv.Itoa(len(body)) + ":" + string(body))
		return stream.SendMsg(&reply)
	}))
	t.Cleanup(srv.Stop)
	go srv.Serve(lis)

	return lis.Addr().String()
}

// messageInjects wraps message rules the way the proxy expects them.
func messageInjects(in ...fault.MessageInject) *fault.Injects {
	return &fault.Injects{Message: in}
}

// call sends one body through the proxy and returns the answer.
func call(t *testing.T, conn *grpc.ClientConn, body string) string {
	t.Helper()
	req := rawBytes(body)
	var resp rawBytes
	if err := conn.Invoke(context.Background(), "/test/Echo", &req, &resp); err != nil {
		t.Fatal(err)
	}
	return string(resp)
}

// The request is cut before the target sees it, and the answer comes back
// untouched: the target reports the short length it received.
func TestProxy_TruncateRequest(t *testing.T) {
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/Echo"},
		Direction:   config.DirectionRequest,
		Probability: 1.0,
		Mutator:     fault.NewTruncate(6),
	})
	p := startProxy(t, startReportEcho(t), injects)
	conn := dialProxy(t, p)

	if got := call(t, conn, "0123456789"); got != "6:012345" {
		t.Fatalf("expected the target to see 6 bytes and the answer to survive, got %q", got)
	}
}

// The request arrives whole -- the target reports all ten bytes -- and the
// answer is cut on the way back.
func TestProxy_TruncateResponse(t *testing.T) {
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/Echo"},
		Direction:   config.DirectionResponse,
		Probability: 1.0,
		Mutator:     fault.NewTruncate(6),
	})
	p := startProxy(t, startReportEcho(t), injects)
	conn := dialProxy(t, p)

	if got := call(t, conn, "0123456789"); got != "10:012" {
		t.Fatalf("expected the target to see 10 bytes and the answer to be cut, got %q", got)
	}
}

// One rule, both legs: the target reports six bytes, and its answer is cut as
// well. The three directions produce three different strings, so this cannot
// pass by accident on a rule that fired on one leg only.
func TestProxy_TruncateBoth(t *testing.T) {
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/Echo"},
		Direction:   config.DirectionBoth,
		Probability: 1.0,
		Mutator:     fault.NewTruncate(6),
	})
	p := startProxy(t, startReportEcho(t), injects)
	conn := dialProxy(t, p)

	if got := call(t, conn, "0123456789"); got != "6:0123" {
		t.Fatalf("expected both legs to be cut, got %q", got)
	}
}

// Corruption reaches the target: the body it echoes back differs from the one
// that was sent, and its length is the same.
func TestProxy_CorruptRequest(t *testing.T) {
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/Echo"},
		Direction:   config.DirectionRequest,
		Probability: 1.0,
		Mutator:     fault.NewCorrupt(1),
	})
	p := startProxy(t, startEcho(t), injects)
	conn := dialProxy(t, p)

	const body = "0123456789abcdef"
	got := call(t, conn, body)
	if got == body {
		t.Fatal("expected a damaged body, got it unchanged")
	}
	if len(got) != len(body) {
		t.Fatalf("expected length %d, got %d", len(body), len(got))
	}
}

// A rule whose pattern does not cover the method leaves the call alone.
func TestProxy_MessageRuleNotMatched(t *testing.T) {
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/other/*"},
		Direction:   config.DirectionRequest,
		Probability: 1.0,
		Mutator:     fault.NewTruncate(2),
	})
	p := startProxy(t, startEcho(t), injects)
	conn := dialProxy(t, p)

	if got := call(t, conn, "hello"); got != "hello" {
		t.Fatalf("expected the body untouched, got %q", got)
	}
}

// Patterns select message rules the same way they select call rules.
func TestProxy_MessageWildcard(t *testing.T) {
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/*"},
		Direction:   config.DirectionRequest,
		Probability: 1.0,
		Mutator:     fault.NewTruncate(2),
	})
	p := startProxy(t, startEcho(t), injects)
	conn := dialProxy(t, p)

	if got := call(t, conn, "hello"); got != "he" {
		t.Fatalf("expected the wildcard rule to cut the body, got %q", got)
	}
}

// A dropped message leaves a hole rather than an error: the stream ends on a
// clean EOF, it just carries nothing. echoStream fails the test on any other
// error, so reaching the comparison means the call itself stayed healthy.
func TestProxy_DropLeavesHoleInStream(t *testing.T) {
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/Echo"},
		Direction:   config.DirectionRequest,
		Probability: 1.0,
		Mutator:     fault.NewDrop(),
	})
	p := startProxy(t, startStreamEcho(t), injects)
	conn := dialProxy(t, p)

	bodies := [][]byte{[]byte("one"), []byte("two"), []byte("three")}
	if got := echoStream(t, conn, bodies); len(got) != 0 {
		t.Fatalf("expected every message dropped, got %d back", len(got))
	}
}

// Mutators run in configuration order: the cut happens first, so the damage
// lands inside what is left rather than in the part already thrown away.
func TestProxy_MutatorChainOrder(t *testing.T) {
	match := config.Match{Method: "/test/Echo"}
	injects := messageInjects(
		fault.MessageInject{Match: match, Direction: config.DirectionRequest,
			Probability: 1.0, Mutator: fault.NewTruncate(4)},
		fault.MessageInject{Match: match, Direction: config.DirectionRequest,
			Probability: 1.0, Mutator: fault.NewCorrupt(1)},
	)
	p := startProxy(t, startEcho(t), injects)
	conn := dialProxy(t, p)

	got := call(t, conn, "0123456789")
	if len(got) != 4 {
		t.Fatalf("expected 4 bytes, got %d (%q)", len(got), got)
	}
	if got == "0123" {
		t.Fatal("expected the remaining bytes to be damaged, got them intact")
	}
}

// spyMutator records how many messages reached it and forwards them unchanged.
type spyMutator struct {
	calls atomic.Int64
}

func (s *spyMutator) Mutate(msg []byte) ([]byte, bool, error) {
	s.calls.Add(1)
	return msg, true, nil
}

// A message that will not be sent has nothing left to change, so the chain
// stops at the mutator that refused to forward it.
func TestProxy_DropStopsChain(t *testing.T) {
	match := config.Match{Method: "/test/Echo"}
	spy := &spyMutator{}
	injects := messageInjects(
		fault.MessageInject{Match: match, Direction: config.DirectionRequest,
			Probability: 1.0, Mutator: fault.NewDrop()},
		fault.MessageInject{Match: match, Direction: config.DirectionRequest,
			Probability: 1.0, Mutator: spy},
	)
	p := startProxy(t, startStreamEcho(t), injects)
	conn := dialProxy(t, p)

	echoStream(t, conn, [][]byte{[]byte("one"), []byte("two")})
	if n := spy.calls.Load(); n != 0 {
		t.Fatalf("expected the chain to stop at the drop, spy saw %d messages", n)
	}
}

// errMutator fails on every message it is given.
type errMutator struct{}

func (errMutator) Mutate([]byte) ([]byte, bool, error) { return nil, false, errors.New("boom") }

// A failure on the request side tears the call down through cancel(), which
// reaches the response side as a bare Canceled. The client must be told what
// actually went wrong instead.
func TestProxy_RequestMutatorErrorReachesClient(t *testing.T) {
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/Echo"},
		Direction:   config.DirectionRequest,
		Probability: 1.0,
		Mutator:     errMutator{},
	})
	p := startProxy(t, startEcho(t), injects)
	conn := dialProxy(t, p)

	req := rawBytes("hello")
	var resp rawBytes
	err := conn.Invoke(context.Background(), "/test/Echo", &req, &resp)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	assertStatus(t, err, codes.Unknown, "boom")
}

// Call-level and message-level faults are kept in separate lists and must both
// still fire on the same call.
func TestProxy_CallAndMessageFault(t *testing.T) {
	match := config.Match{Method: "/test/Echo"}
	injects := &fault.Injects{
		Call: []fault.Inject{{Match: match, Probability: 1.0,
			Fault: fault.NewDelay(100 * time.Millisecond)}},
		Message: []fault.MessageInject{
			{Match: match, Direction: config.DirectionRequest,
				Probability: 1.0, Mutator: fault.NewTruncate(2)},
		},
	}
	p := startProxy(t, startEcho(t), injects)
	conn := dialProxy(t, p)

	start := time.Now()
	got := call(t, conn, "hello")
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("expected delay >= 100ms, got %v", elapsed)
	}
	if got != "he" {
		t.Errorf("expected the body cut to %q, got %q", "he", got)
	}
}

// Hot reload replaces message rules as well as call rules.
func TestProxy_SetInjectsMessage(t *testing.T) {
	p := startProxy(t, startEcho(t), nil)
	conn := dialProxy(t, p)

	if got := call(t, conn, "hello"); got != "hello" {
		t.Fatalf("expected the body untouched before reload, got %q", got)
	}

	p.SetInjects(messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/Echo"},
		Direction:   config.DirectionRequest,
		Probability: 1.0,
		Mutator:     fault.NewTruncate(2),
	}))

	if got := call(t, conn, "hello"); got != "he" {
		t.Fatalf("expected the new rule to cut the body, got %q", got)
	}
}

// selectMutators splits one list into two by direction. both belongs on either
// leg, so the two checks are independent: folding them into one if/else would
// quietly reduce both to request.
func TestSelectMutators(t *testing.T) {
	match := config.Match{Method: "/test/Echo"}
	rules := []fault.MessageInject{
		{Match: match, Name: "on-request", Direction: config.DirectionRequest, Mutator: fault.NewDrop()},
		{Match: match, Name: "on-response", Direction: config.DirectionResponse, Mutator: fault.NewDrop()},
		{Match: match, Name: "on-both", Direction: config.DirectionBoth, Mutator: fault.NewDrop()},
		{Match: config.Match{Method: "/other/Call"}, Name: "elsewhere", Direction: config.DirectionBoth, Mutator: fault.NewDrop()},
	}

	req, resp := selectMutators(rules, "/test/Echo")

	if got := names(req); got != "on-request,on-both" {
		t.Errorf("request leg = %s, want on-request,on-both", got)
	}
	if got := names(resp); got != "on-response,on-both" {
		t.Errorf("response leg = %s, want on-response,on-both", got)
	}
}

// names lists the rules of one leg in the order they will run.
func names(rules []fault.MessageInject) string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Name)
	}
	return strings.Join(out, ",")
}

// The dice are rolled by the proxy now, so a rule at zero has to be selected
// and then skipped on every message rather than never selected at all.
func TestProxy_ZeroProbabilityNeverFires(t *testing.T) {
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/Echo"},
		Name:        "never",
		Direction:   config.DirectionRequest,
		Probability: 0,
		Mutator:     fault.NewTruncate(2),
	})
	p := startProxy(t, startEcho(t), injects)
	conn := dialProxy(t, p)

	if got := call(t, conn, "hello"); got != "hello" {
		t.Fatalf("expected the body untouched, got %q", got)
	}
}

// captureLogs points the default logger at a buffer for the duration of one
// test. Both legs write to it from their own goroutines, hence the lock.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLogs(t *testing.T) *logBuffer {
	t.Helper()
	var b logBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &b
}

// What a leg did is reported once, when it is done, rather than once per
// message: a stream of three carries one line saying the rule fired three
// times.
func TestProxy_LogsFiredRules(t *testing.T) {
	logs := captureLogs(t)
	injects := messageInjects(fault.MessageInject{
		Match:       config.Match{Method: "/test/Echo"},
		Name:        "cut-me",
		Direction:   config.DirectionRequest,
		Probability: 1.0,
		Mutator:     fault.NewTruncate(2),
	})
	p := startProxy(t, startStreamEcho(t), injects)
	conn := dialProxy(t, p)

	echoStream(t, conn, [][]byte{[]byte("one"), []byte("two"), []byte("three")})

	out := logs.String()
	if !strings.Contains(out, `"msg":"mutations"`) {
		t.Fatalf("expected a mutations line, got:\n%s", out)
	}
	if !strings.Contains(out, `"cut-me":3`) {
		t.Errorf("expected the rule to be reported as fired 3 times, got:\n%s", out)
	}
	if strings.Count(out, `"msg":"mutations"`) != 1 {
		t.Errorf("expected one line for the one leg carrying rules, got:\n%s", out)
	}
	// slog renders a raw time.Duration as a nanosecond count, which is not what
	// the logs are meant to read like.
	if !strings.Contains(out, `"duration":"`) {
		t.Errorf("expected the duration to be logged as a readable string, got:\n%s", out)
	}
}

// A call the rules do not touch says nothing: two empty lines per call would
// bury the line that matters.
func TestProxy_QuietWithoutRules(t *testing.T) {
	logs := captureLogs(t)
	p := startProxy(t, startEcho(t), nil)
	conn := dialProxy(t, p)

	call(t, conn, "hello")

	if out := logs.String(); strings.Contains(out, `"msg":"mutations"`) {
		t.Errorf("expected no mutations line, got:\n%s", out)
	}
}
