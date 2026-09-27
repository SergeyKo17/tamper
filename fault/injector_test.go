package fault

import (
	"fmt"
	"testing"
	"time"

	"github.com/SergeyKo17/tamper/config"
)

func TestNew_Delay(t *testing.T) {
	rules := []config.Rule{
		{
			Match: config.Match{Method: "/pkg.Svc/Get"},
			Fault: config.Fault{Type: config.TypeDelay, Duration: time.Second, Probability: 0.5},
		},
	}
	injs, err := New(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(injs.Call) != 1 {
		t.Fatalf("expected 1 injector, got %d", len(injs.Call))
	}
	if _, ok := injs.Call[0].Fault.(*Delay); !ok {
		t.Fatalf("expected *Delay, got %T", injs.Call[0].Fault)
	}
}

func TestNew_Abort(t *testing.T) {
	rules := []config.Rule{
		{
			Match: config.Match{Method: "/pkg.Svc/Get"},
			Fault: config.Fault{Type: config.TypeAbort, Code: 14, Message: "down", Probability: 1.0},
		},
	}
	injs, err := New(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(injs.Call) != 1 {
		t.Fatalf("expected 1 injector, got %d", len(injs.Call))
	}
	if _, ok := injs.Call[0].Fault.(*Abort); !ok {
		t.Fatalf("expected *Abort, got %T", injs.Call[0].Fault)
	}
}

func TestNew_UnknownType(t *testing.T) {
	rules := []config.Rule{
		{
			Match: config.Match{Method: "/pkg.Svc/Get"},
			Fault: config.Fault{Type: "chaos"},
		},
	}
	_, err := New(rules)
	if err == nil {
		t.Fatal("expected error for unknown fault type")
	}
}

func TestNew_MixedRules(t *testing.T) {
	rules := []config.Rule{
		{
			Match: config.Match{Method: "/pkg.Svc/Get"},
			Fault: config.Fault{Type: config.TypeDelay, Duration: time.Second, Probability: 0.5},
		},
		{
			Match: config.Match{Method: "/pkg.Svc/Delete"},
			Fault: config.Fault{Type: config.TypeAbort, Code: 13, Message: "internal", Probability: 1.0},
		},
	}
	injs, err := New(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(injs.Call) != 2 {
		t.Fatalf("expected 2 injectors, got %d", len(injs.Call))
	}
	if _, ok := injs.Call[0].Fault.(*Delay); !ok {
		t.Fatalf("expected *Delay, got %T", injs.Call[0].Fault)
	}
	if _, ok := injs.Call[1].Fault.(*Abort); !ok {
		t.Fatalf("expected *Abort, got %T", injs.Call[1].Fault)
	}
}

// A rule carries its own name into the built injects, and one without a name
// is called after its type and its place in the file.
func TestNew_RuleNames(t *testing.T) {
	rules := []config.Rule{
		{Fault: config.Fault{Type: config.TypeDelay, Duration: time.Second}},
		{Fault: config.Fault{Type: config.TypeAbort, Code: 13, Name: "kill-it"}},
		{Fault: config.Fault{Type: config.TypeDrop}},
	}
	injs, err := New(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := injs.Call[0].Name; got != "delay-0" {
		t.Errorf("unnamed rule = %q, want %q", got, "delay-0")
	}
	if got := injs.Call[1].Name; got != "kill-it" {
		t.Errorf("named rule = %q, want %q", got, "kill-it")
	}
	if got := injs.Message[0].Name; got != "drop-2" {
		t.Errorf("unnamed message rule = %q, want %q", got, "drop-2")
	}
}

// The two ends of the range are certainties, and the proxy leans on that: a
// rule at 1.0 has to fire on every message, one at 0.0 on none. Neither rule
// here is given a dice, so this also covers the fallback to the global one.
func TestFires(t *testing.T) {
	never := Inject{Probability: 0}
	always := Inject{Probability: 1}
	neverMsg := MessageInject{Probability: 0}
	alwaysMsg := MessageInject{Probability: 1}

	for range 100 {
		if never.Fires() {
			t.Fatal("probability 0 fired")
		}
		if !always.Fires() {
			t.Fatal("probability 1 did not fire")
		}
		if neverMsg.Fires() {
			t.Fatal("message rule at probability 0 fired")
		}
		if !alwaysMsg.Fires() {
			t.Fatal("message rule at probability 1 did not fire")
		}
	}
}

// newMutator is reached through New, which gates it on IsMessageFault, so its
// three types are what a configuration can ask for and the last case is
// defensive: it answers the type list drifting apart between the two packages.
func TestNewMutator(t *testing.T) {
	cases := []struct {
		faultType string
		want      Mutator
		wantErr   bool
	}{
		{faultType: config.TypeCorrupt, want: Corrupt{}},
		{faultType: config.TypeTruncate, want: Truncate{}},
		{faultType: config.TypeDrop, want: Drop{}},
		{faultType: "nonsense", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.faultType, func(t *testing.T) {
			got, err := newMutator(config.Fault{Type: c.faultType})
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %T", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", c.want) {
				t.Errorf("mutator = %T, want %T", got, c.want)
			}
		})
	}
}
