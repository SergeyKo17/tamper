package fault

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Abort is a fault injector that returns a gRPC error immediately.
type Abort struct {
	code    int
	message string
}

// NewAbort creates an Abort injector with the given gRPC code and message.
func NewAbort(code int, msg string) *Abort {
	return &Abort{code: code, message: msg}
}

// Apply returns a gRPC status error.
func (a *Abort) Apply(ctx context.Context) (context.Context, error) {
	if ctx.Err() != nil {
		return ctx, ctx.Err()
	}
	return ctx, status.Error(codes.Code(a.code), a.message) //nolint:gosec
}
