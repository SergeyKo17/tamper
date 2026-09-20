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
