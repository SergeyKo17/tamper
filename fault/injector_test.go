package fault

import (
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
// rule at 1.0 has to fire on every message, one at 0.0 on none.
func TestFires(t *testing.T) {
	for range 100 {
		if Fires(0) {
			t.Fatal("probability 0 fired")
		}
		if !Fires(1) {
			t.Fatal("probability 1 did not fire")
		}
	}
}
