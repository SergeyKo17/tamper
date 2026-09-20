package fault

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/SergeyKo17/tamper/config"
)

// Injects holds the active rules, split by the stage they act on.
type Injects struct {
	Call    []Inject        // delay, abort — до отправки
	Message []MessageInject // truncate, corrupt, drop — на каждое сообщение
}

// Inject pairs a request matcher with a fault injector.
type Inject struct {
	Match config.Match
	Fault Injector
}

// MessageInject pairs a matcher with a mutator and the leg it works on.
type MessageInject struct {
	Match     config.Match
	Direction string
	Mutator   Mutator
}

// Injector applies a fault to a gRPC request.
type Injector interface {
	Apply(context.Context) (context.Context, error)
}

// Mutator changes a single message in flight. It reports whether the message
// should still be forwarded: a dropped message is not relayed at all.
type Mutator interface {
	Mutate([]byte) ([]byte, bool, error)
}

type chance float64

// fires rolls the dice for one application of a fault.
func (c chance) fires() bool { return rand.Float64() < float64(c) } //nolint:gosec

// New builds the active rules from configuration, splitting call-level faults
// from those that work on individual messages.
func New(rules []config.Rule) (*Injects, error) {
	var in Injects
	for _, r := range rules {
		switch {
		case config.IsMessageFault(r.Fault.Type):
			m, err := newMutator(r.Fault)
			if err != nil {
				return nil, err
			}
			in.Message = append(in.Message, MessageInject{Match: r.Match, Direction: r.Fault.Direction, Mutator: m})
		default:
			f, err := newInjector(r.Fault)
			if err != nil {
				return nil, err
			}
			in.Call = append(in.Call, Inject{Match: r.Match, Fault: f})
		}
	}
	return &in, nil
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

func newMutator(fault config.Fault) (Mutator, error) {
	switch fault.Type {
	case config.TypeCorrupt:
		return NewCorrupt(fault.Count, fault.Probability), nil
	case config.TypeTruncate:
		return NewTruncate(fault.Size, fault.Probability), nil
	case config.TypeDrop:
		return NewDrop(fault.Probability), nil
	default:
		return nil, fmt.Errorf("unsupported fault type: %s", fault.Type)
	}
}
