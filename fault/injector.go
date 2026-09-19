package fault

import (
	"context"
	"fmt"

	"github.com/SergeyKo17/tamper/config"
)

// Inject pairs a request matcher with a fault injector.
type Inject struct {
	Match config.Match
	Fault Injector
}

// Injector applies a fault to a gRPC request.
type Injector interface {
	Apply(context.Context) (context.Context, error)
}

// NewInjectors creates injectors from configuration rules.
func NewInjectors(rules []config.Rule) ([]Inject, error) {
	injs := make([]Inject, len(rules))
	for i, r := range rules {
		inj, err := newInjector(r.Fault)
		if err != nil {
			return nil, err
		}

		injs[i] = Inject{Match: r.Match, Fault: inj}
	}

	return injs, nil
}

func newInjector(fault config.Fault) (Injector, error) {
	switch fault.Type {
	case config.TypeDelay:
		return NewDelay(fault.Duration, fault.Probability), nil
	case config.TypeAbort:
		return NewAbort(fault.Code, fault.Message, fault.Probability), nil
	default:
		return nil, fmt.Errorf("unsupported fault type: %s", fault.Type)
	}
}
