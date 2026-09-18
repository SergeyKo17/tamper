package config

import (
	"os"
	"path/filepath"
	"testing"
)

func tmpConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tamper.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNew_ValidConfig(t *testing.T) {
	yaml := `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
rules:
  - match:
      method: "/pkg.Service/Method"
    fault:
      type: delay
      prob: 0.5
      duration: 500ms
  - match:
      method: "/pkg.Service/Other"
    fault:
      type: abort
      code: 14
      msg: "unavailable"
      prob: 1.0
`
	cfg, err := Load(tmpConfigFile(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen.Addr != ":9090" {
		t.Errorf("listen = %q, want %q", cfg.Listen.Addr, ":9090")
	}
	if cfg.Target.Addr != "localhost:50051" {
		t.Errorf("target = %q, want %q", cfg.Target.Addr, "localhost:50051")
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("rules count = %d, want 2", len(cfg.Rules))
	}
	if cfg.Rules[0].Fault.Type != "delay" {
		t.Errorf("rule[0] type = %q, want %q", cfg.Rules[0].Fault.Type, "delay")
	}
	if cfg.Rules[0].Fault.Probability != 0.5 {
		t.Errorf("rule[0] probability = %f, want 0.5", cfg.Rules[0].Fault.Probability)
	}
	if cfg.Rules[1].Fault.Code != 14 {
		t.Errorf("rule[1] code = %d, want 14", cfg.Rules[1].Fault.Code)
	}
}

func TestNew_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/tamper.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNew_InvalidYAML(t *testing.T) {
	_, err := Load(tmpConfigFile(t, "{{invalid"))
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestNew_EmptyListen(t *testing.T) {
	yaml := `
target:
  addr: "localhost:50051"
`
	_, err := Load(tmpConfigFile(t, yaml))
	if err == nil {
		t.Fatal("expected error for empty listen")
	}
}

func TestNew_EmptyTarget(t *testing.T) {
	yaml := `
listen:
  addr: ":9090"
`
	_, err := Load(tmpConfigFile(t, yaml))
	if err == nil {
		t.Fatal("expected error for empty target")
	}
}

func TestNew_UnsupportedFaultType(t *testing.T) {
	yaml := `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
rules:
  - match:
      method: "/pkg.Service/Method"
    fault:
      type: mutate
      prob: 0.5
`
	_, err := Load(tmpConfigFile(t, yaml))
	if err == nil {
		t.Fatal("expected error for unsupported fault type")
	}
}

func TestNew_ProbabilityOutOfRange(t *testing.T) {
	yaml := `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
rules:
  - match:
      method: "/pkg.Service/Method"
    fault:
      type: delay
      prob: 1.5
`
	_, err := Load(tmpConfigFile(t, yaml))
	if err == nil {
		t.Fatal("expected error for probability > 1")
	}
}

func TestNew_NegativeProbability(t *testing.T) {
	yaml := `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
rules:
  - match:
      method: "/pkg.Service/Method"
    fault:
      type: abort
      prob: -0.1
`
	_, err := Load(tmpConfigFile(t, yaml))
	if err == nil {
		t.Fatal("expected error for negative probability")
	}
}

func TestNew_NoRules(t *testing.T) {
	yaml := `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
`
	cfg, err := Load(tmpConfigFile(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 0 {
		t.Errorf("rules count = %d, want 0", len(cfg.Rules))
	}
}
