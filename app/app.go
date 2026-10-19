package app

import (
	"context"
	"log/slog"

	"github.com/SergeyKo17/tamper/config"
	"github.com/SergeyKo17/tamper/fault"
	"github.com/SergeyKo17/tamper/proxy"
)

// New loads the configuration, creates fault injectors, and assembles the
// proxy. The level the configuration asks for is written into lvl, which the
// caller's handler reads, and every reload keeps it current. The watcher comes
// back so the caller can trigger a reload of its own.
func New(ctx context.Context, path string, lvl *slog.LevelVar) (*proxy.Proxy, *config.Watcher, error) {
	watcher, err := config.Watch(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	cfg := watcher.Config()
	injectors, err := fault.New(cfg.Rules)
	if err != nil {
		return nil, nil, err
	}
	setLevel(lvl, cfg.Log.Level)

	proxy, err := proxy.New(ctx, cfg, injectors)
	if err != nil {
		return nil, nil, err
	}

	go func() {
		for {
			select {
			case <-watcher.Updates():
				cfg = watcher.Config()
				setLevel(lvl, cfg.Log.Level)
				newInjects, err := fault.New(cfg.Rules)
				if err != nil {
					slog.Error("load config", "err", err)
					continue
				}
				proxy.SetInjects(newInjects)
			case <-ctx.Done():
				return
			}
		}
	}()
	return proxy, watcher, nil
}

// setLevel moves a configured level into lvl. A level the configuration got
// wrong is reported and left at whatever was in force, which keeps the proxy
// running on a config it can otherwise serve.
func setLevel(lvl *slog.LevelVar, level string) {
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		slog.Error("set log level", "err", err)
	}
}
