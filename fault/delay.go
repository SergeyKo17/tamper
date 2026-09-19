package fault

import (
	"context"
	"math/rand/v2"
	"time"
)

// Delay is a fault injector that adds latency before forwarding the request.
type Delay struct {
	duration    time.Duration
	probability float64
}

// NewDelay creates a Delay injector with the given duration and probability.
func NewDelay(duration time.Duration, probability float64) *Delay {
	return &Delay{duration: duration, probability: probability}
}

// Apply sleeps for the configured duration based on probability.
func (d *Delay) Apply(ctx context.Context) (context.Context, error) {
	if rand.Float64() >= d.probability { //nolint:gosec
		return ctx, nil
	}
	select {
	case <-time.After(d.duration):
		return ctx, nil
	case <-ctx.Done():
		return ctx, ctx.Err()
	}
}
