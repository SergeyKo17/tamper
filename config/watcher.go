package config

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a configuration file for changes and atomically swaps the config on reload.
type Watcher struct {
	cfg atomic.Pointer[Config]
	// path is kept because a reload is no longer only the watcher's own
	// business: Reload is called from the outside too, and has nowhere else to
	// read the file from.
	path string
	// update carries one announcement per reload. The buffer is what makes a
	// reload independent of whether anyone is listening yet.
	update chan struct{}
}

// Watch parses the config at path and starts a background goroutine that reloads it on file changes.
func Watch(ctx context.Context, path string) (*Watcher, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("get absolute config path: %w", err)
	}
	absDir := filepath.Dir(absPath)

	w := Watcher{path: absPath, update: make(chan struct{}, 1)}
	w.cfg.Store(cfg)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}
	err = watcher.Add(absDir)
	if err != nil {
		if err := watcher.Close(); err != nil {
			slog.Error("watcher close", "err", err)
		}
		return nil, fmt.Errorf("add watcher path: %w", err)
	}

	go w.run(ctx, watcher)

	return &w, nil
}

func (w *Watcher) run(ctx context.Context, watcher *fsnotify.Watcher) {
	defer func() {
		if err := watcher.Close(); err != nil {
			slog.Error("watcher close", "err", err)
		}
	}()
	timer := time.NewTimer(0)
	<-timer.C
	defer timer.Stop()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				slog.Info("watcher events channel closed")
				return
			}
			if event.Op == fsnotify.Chmod {
				continue
			}
			timer.Reset(100 * time.Millisecond)
		case err, ok := <-watcher.Errors:
			if !ok {
				slog.Info("watcher stopped")
				return
			}
			if err != nil {
				slog.Error("watcher", "err", err)
			}
		case <-timer.C:
			if err := w.Reload(); err != nil {
				slog.Error("read config", "err", err)
			}
		case <-ctx.Done():
			slog.Info("Watch close by context")
			return
		}
	}
}

// Reload rereads the configuration file and publishes it. It is also the only
// reload a process gets where the file system reports no change at all: a
// config bind-mounted from a Windows host delivers no events to a container,
// so the proxy there is reloaded by signal instead.
func (w *Watcher) Reload() error {
	cfg, err := Load(w.path)
	if err != nil {
		return err
	}
	w.cfg.Store(cfg)
	select {
	case w.update <- struct{}{}:
	default:
	}
	return nil
}

// Updates announces every reload, whatever triggered it. It is a single
// nudge, not a queue of versions: a reader that misses one still gets the
// latest configuration from Config.
func (w *Watcher) Updates() <-chan struct{} {
	return w.update
}

// Config returns the current configuration.
func (w *Watcher) Config() *Config {
	return w.cfg.Load()
}
