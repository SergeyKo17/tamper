package fault

import (
	"context"
	"time"
)

// Delay is a fault injector that adds latency before forwarding the request.
type Delay struct {
	duration time.Duration
}

// NewDelay creates a Delay injector with the given duration.
func NewDelay(duration time.Duration) *Delay {
	return &Delay{duration: duration}
}

// Apply sleeps for the configured duration.
func (d *Delay) Apply(ctx context.Context) (context.Context, error) {
	select {
	case <-time.After(d.duration):
		return ctx, nil
	case <-ctx.Done():
		return ctx, ctx.Err()
	}
}
