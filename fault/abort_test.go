package fault

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAbort_ApplyTriggered(t *testing.T) {
	a := NewAbort(14, "unavailable", 1.0)
	err := a.Apply(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.Unavailable {
		t.Fatalf("expected code Unavailable, got %v", st.Code())
	}
	if st.Message() != "unavailable" {
		t.Fatalf("expected message 'unavailable', got %q", st.Message())
	}
}

func TestAbort_ApplySkipped(t *testing.T) {
	a := NewAbort(14, "unavailable", 0.0)
	err := a.Apply(context.Background())
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestAbort_ApplyCanceledCtx(t *testing.T) {
	a := NewAbort(14, "unavailable", 1.0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.Apply(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
