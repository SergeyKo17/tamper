package config

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestWatch_LoadsInitialConfig(t *testing.T) {
	path := tmpConfigFile(t, `
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
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := Watch(ctx, path, make(chan struct{}, 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := w.Config()
	if cfg.Listen.Addr != ":9090" {
		t.Errorf("listen = %q, want %q", cfg.Listen.Addr, ":9090")
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("rules count = %d, want 1", len(cfg.Rules))
	}
}

func TestWatch_ReloadsOnFileChange(t *testing.T) {
	path := tmpConfigFile(t, `
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
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := Watch(ctx, path, make(chan struct{}, 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newYAML := `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
rules:
  - match:
      method: "/pkg.Service/Method"
    fault:
      type: abort
      code: 14
      prob: 1.0
  - match:
      method: "/pkg.Service/Other"
    fault:
      type: delay
      prob: 0.3
      duration: 200ms
`
	if err := os.WriteFile(path, []byte(newYAML), 0644); err != nil {
		t.Fatalf("write new config: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("config was not reloaded within 2s")
		default:
			if cfg := w.Config(); len(cfg.Rules) == 2 {
				if cfg.Rules[0].Fault.Type != "abort" {
					t.Errorf("rule[0] type = %q, want %q", cfg.Rules[0].Fault.Type, "abort")
				}
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestWatch_InvalidFileKeepsOldConfig(t *testing.T) {
	path := tmpConfigFile(t, `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := Watch(ctx, path, make(chan struct{}, 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := os.WriteFile(path, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	cfg := w.Config()
	if cfg.Listen.Addr != ":9090" {
		t.Errorf("config should be unchanged, listen = %q", cfg.Listen.Addr)
	}
}

func TestWatch_InvalidPath(t *testing.T) {
	ctx := context.Background()
	_, err := Watch(ctx, "/nonexistent/tamper.yaml", make(chan struct{}, 1))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWatch_CancelStopsWatcher(t *testing.T) {
	path := tmpConfigFile(t, `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
`)
	ctx, cancel := context.WithCancel(context.Background())

	_, err := Watch(ctx, path, make(chan struct{}, 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
}
