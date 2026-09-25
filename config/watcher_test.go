package config

import (
	"context"
	"fmt"
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

// configWithAddr renders a minimal valid config listening on addr.
func configWithAddr(addr string) string {
	return fmt.Sprintf("\nlisten:\n  addr: %q\ntarget:\n  addr: \"localhost:50051\"\n", addr)
}

// replaceFile swaps the file at path the way an editor or Kubernetes does it:
// write next to it, then rename over. The file that was there is not written
// into, it is replaced by a different one under the same name.
func replaceFile(t *testing.T, path, content string) {
	t.Helper()
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("replace config: %v", err)
	}
}

// waitForAddr blocks until the watcher reports a config listening on addr.
func waitForAddr(t *testing.T, w *Watcher, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("config with listen %q was not loaded within 2s, got %q", want, w.Config().Listen.Addr)
		default:
			if w.Config().Listen.Addr == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// A config is usually replaced rather than written into, and a watch on the
// file itself does not survive that: the file stays alive only as long as we
// hold it, and nothing will ever change it again.
//
// One replacement is not enough to show this. The dying file reports its own
// removal on the way out, which looks exactly like a reason to reload -- so a
// broken watcher gets the first one right and goes deaf afterwards. The second
// change is what tells the two apart.
//
// On Windows this passes either way: there a watch on a file is served by the
// directory underneath, so replacing the file keeps working.
func TestWatch_SurvivesFileReplacement(t *testing.T) {
	path := tmpConfigFile(t, configWithAddr(":9090"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := Watch(ctx, path, make(chan struct{}, 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	replaceFile(t, path, configWithAddr(":7070"))
	waitForAddr(t, w, ":7070")

	replaceFile(t, path, configWithAddr(":6060"))
	waitForAddr(t, w, ":6060")
}
