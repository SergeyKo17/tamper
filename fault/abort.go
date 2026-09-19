package fault

import (
	"context"
	"math/rand/v2"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Abort is a fault injector that returns a gRPC error immediately.
type Abort struct {
	code        int
	message     string
	probability float64
}

// NewAbort creates an Abort injector with the given gRPC code, message and probability.
func NewAbort(code int, msg string, probability float64) *Abort {
	return &Abort{code: code, message: msg, probability: probability}
}

// Apply returns a gRPC status error based on probability.
func (a *Abort) Apply(ctx context.Context) (context.Context, error) {
	if ctx.Err() != nil {
		return ctx, ctx.Err()
	}
	if rand.Float64() >= a.probability { //nolint:gosec
		return ctx, nil
	}
	return ctx, status.Error(codes.Code(a.code), a.message) //nolint:gosec
}
