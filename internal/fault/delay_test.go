package fault

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDelay_ApplyTriggered(t *testing.T) {
	d := NewDelay(100 * time.Millisecond)
	start := time.Now()
	_, err := d.Apply(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("expected delay >= 100ms, got %v", elapsed)
	}
}

func TestDelay_ApplyCanceled(t *testing.T) {
	d := NewDelay(5 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := d.Apply(ctx)
	if elapsed := time.Since(start); elapsed >= 50*time.Millisecond {
		t.Fatalf("expected immediate return on canceled ctx, got %v", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
