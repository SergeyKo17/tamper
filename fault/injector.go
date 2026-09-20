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

// Inject pairs a request matcher with a fault injector. Name and Probability
// belong to the rule rather than to the injector: the dice are rolled by the
// caller, which is then the one that knows whether the fault fired and under
// which name to report it.
type Inject struct {
	Match       config.Match
	Name        string
	Probability float64
	Fault       Injector
}

// MessageInject pairs a matcher with a mutator and the leg it works on.
type MessageInject struct {
	Match       config.Match
	Name        string
	Direction   string
	Probability float64
	Mutator     Mutator
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

// Fires rolls the dice for one application of a fault.
func Fires(probability float64) bool { return rand.Float64() < probability } //nolint:gosec

// New builds the active rules from configuration, splitting call-level faults
// from those that work on individual messages.
func New(rules []config.Rule) (*Injects, error) {
	var in Injects
	for i, r := range rules {
		name := ruleName(r.Fault, i)
		switch {
		case config.IsMessageFault(r.Fault.Type):
			m, err := newMutator(r.Fault)
			if err != nil {
				return nil, err
			}
			in.Message = append(in.Message, MessageInject{
				Match:       r.Match,
				Name:        name,
				Direction:   r.Fault.Direction,
				Probability: r.Fault.Probability,
				Mutator:     m,
			})
		default:
			f, err := newInjector(r.Fault)
			if err != nil {
				return nil, err
			}
			in.Call = append(in.Call, Inject{
				Match:       r.Match,
				Name:        name,
				Probability: r.Fault.Probability,
				Fault:       f,
			})
		}
	}
	return &in, nil
}

// ruleName is what a fired rule is reported as. A rule with no name of its own
// is called after its type and its position in the file, which is enough to
// find it there.
func ruleName(f config.Fault, i int) string {
	if f.Name != "" {
		return f.Name
	}
	return fmt.Sprintf("%s-%d", f.Type, i)
}

func newInjector(fault config.Fault) (Injector, error) {
	switch fault.Type {
	case config.TypeDelay:
		return NewDelay(fault.Duration), nil
	case config.TypeAbort:
		return NewAbort(fault.Code, fault.Message), nil
	default:
		return nil, fmt.Errorf("unsupported fault type: %s", fault.Type)
	}
}

func newMutator(fault config.Fault) (Mutator, error) {
	switch fault.Type {
	case config.TypeCorrupt:
		return NewCorrupt(fault.Count), nil
	case config.TypeTruncate:
		return NewTruncate(fault.Size), nil
	case config.TypeDrop:
		return NewDrop(), nil
	default:
		return nil, fmt.Errorf("unsupported fault type: %s", fault.Type)
	}
}
