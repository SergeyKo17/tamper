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
}

// Watch parses the config at path and starts a background goroutine that reloads it on file changes.
func Watch(ctx context.Context, path string, update chan<- struct{}) (*Watcher, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("get absolute config path: %w", err)
	}
	absDir := filepath.Dir(absPath)

	var w Watcher
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

	go w.run(ctx, watcher, absPath, update)

	return &w, nil
}

func (w *Watcher) run(ctx context.Context, watcher *fsnotify.Watcher, path string, update chan<- struct{}) {
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
			cfg, err := Load(path)
			if err != nil {
				slog.Error("read config", "err", err)
				continue
			}
			w.cfg.Store(cfg)
			select {
			case update <- struct{}{}:
			default:
			}
		case <-ctx.Done():
			slog.Info("Watch close by context")
			return
		}
	}
}

// Config returns the current configuration.
func (w *Watcher) Config() *Config {
	return w.cfg.Load()
}
