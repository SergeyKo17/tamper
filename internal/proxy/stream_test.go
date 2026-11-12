package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
)

// startStreamEcho runs a target that echoes every message of a stream back.
func startStreamEcho(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		for {
			var msg rawBytes
			if err := stream.RecvMsg(&msg); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			if err := stream.SendMsg(&msg); err != nil {
				return err
			}
		}
	}))
	t.Cleanup(srv.Stop)
	go srv.Serve(lis)

	return lis.Addr().String()
}

// payload builds a body larger than mem.BufferPoolingThreshold (1 KiB), so the
// transport takes its buffers from the pool.
func payload(seed byte, size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = seed
	}
	return b
}

// echoStream sends every body over one stream and returns what came back.
func echoStream(t *testing.T, conn *grpc.ClientConn, bodies [][]byte) [][]byte {
	t.Helper()
	desc := &grpc.StreamDesc{ServerStreams: true, ClientStreams: true}
	cs, err := grpc.NewClientStream(context.Background(), desc, conn, "/test/Echo")
	if err != nil {
		t.Fatal(err)
	}

	got := make([][]byte, 0, len(bodies))
	for _, body := range bodies {
		req := rawBytes(body)
		if err := cs.SendMsg(&req); err != nil {
			t.Fatal(err)
		}
	}
	if err := cs.CloseSend(); err != nil {
		t.Fatal(err)
	}
	for {
		var resp rawBytes
		if err := cs.RecvMsg(&resp); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
		got = append(got, bytes.Clone(resp))
	}
	return got
}

// TestProxy_ClientSendsAfterTargetAnswered drives the window the request pump
// outlives the handler in: the target answers the first message and ends the
// call while the client is still sending, so the pump is left in a RecvMsg on a
// stream grpc-go has already finished. Nothing here can assert that window is
// safe — that is what -race watches for on CI.
func TestProxy_ClientSendsAfterTargetAnswered(t *testing.T) {
	p := startProxy(t, startEcho(t), nil)
	conn := dialProxy(t, p)

	desc := &grpc.StreamDesc{ServerStreams: true, ClientStreams: true}
	cs, err := conn.NewStream(context.Background(), desc, "/test/Echo")
	if err != nil {
		t.Fatal(err)
	}

	// The sending never stops on its own, so messages are still arriving when
	// the handler returns.
	body := rawBytes(payload('x', 2048))
	go func() {
		for {
			if err := cs.SendMsg(&body); err != nil {
				return
			}
		}
	}()

	var resp rawBytes
	if err := cs.RecvMsg(&resp); err != nil {
		t.Fatalf("the echoed message never arrived: %v", err)
	}
	if !bytes.Equal(resp, body) {
		t.Errorf("expected %d bytes back, got %d", len(body), len(resp))
	}

	// The target returned without an error, so the call ends with OK however
	// long the client went on sending into it.
	if err := cs.RecvMsg(&resp); !errors.Is(err, io.EOF) {
		t.Errorf("expected the call to end with OK, got %v", err)
	}

	if got := echoStream(t, conn, [][]byte{payload('y', 2048)}); len(got) != 1 {
		t.Errorf("expected the next call to still work, got %d messages back", len(got))
	}
}

func TestProxy_LargePayload(t *testing.T) {
	p := startProxy(t, startStreamEcho(t), nil)
	conn := dialProxy(t, p)

	want := payload('a', 64*1024)
	got := echoStream(t, conn, [][]byte{want})
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if !bytes.Equal(got[0], want) {
		t.Errorf("payload corrupted: %d bytes in, %d bytes out", len(want), len(got[0]))
	}
}

func TestProxy_StreamManyMessages(t *testing.T) {
	p := startProxy(t, startStreamEcho(t), nil)
	conn := dialProxy(t, p)

	const count = 16
	bodies := make([][]byte, count)
	for i := range bodies {
		bodies[i] = payload(byte('a'+i), 8*1024)
	}

	got := echoStream(t, conn, bodies)
	if len(got) != count {
		t.Fatalf("expected %d messages, got %d", count, len(got))
	}
	for i := range bodies {
		if !bytes.Equal(got[i], bodies[i]) {
			t.Errorf("message %d corrupted", i)
		}
	}
}

func TestProxy_ConcurrentLargePayloads(t *testing.T) {
	p := startProxy(t, startStreamEcho(t), nil)

	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan string, workers)

	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := dialProxy(t, p)
			want := payload(byte(i), 32*1024)
			got := echoStream(t, conn, [][]byte{want})
			if len(got) != 1 {
				errs <- fmt.Sprintf("worker %d: expected 1 message, got %d", i, len(got))
				return
			}
			if !bytes.Equal(got[0], want) {
				errs <- fmt.Sprintf("worker %d: payload corrupted", i)
			}
		}()
	}
	wg.Wait()
	close(errs)

	for msg := range errs {
		t.Error(msg)
	}
}
