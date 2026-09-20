package app

import (
	"context"
	"log/slog"
	"os"

	"github.com/SergeyKo17/tamper/config"
	"github.com/SergeyKo17/tamper/fault"
	"github.com/SergeyKo17/tamper/proxy"
)

// New loads the configuration, creates fault injectors, and assembles the proxy.
func New(ctx context.Context, path string) (*proxy.Proxy, error) {
	var slogLvl slog.LevelVar
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &slogLvl})))

	updates := make(chan struct{}, 1)
	watcher, err := config.Watch(ctx, path, updates)
	if err != nil {
		return nil, err
	}
	cfg := watcher.Config()
	injectors, err := fault.New(cfg.Rules)
	if err != nil {
		return nil, err
	}
	if err := slogLvl.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		slog.Error("set log level", "err", err)
	}

	proxy, err := proxy.New(ctx, cfg, injectors)
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			select {
			case <-updates:
				cfg = watcher.Config()
				if err := slogLvl.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
					slog.Error("set log level", "err", err)
				}
				newInjects, err := fault.New(cfg.Rules)
				if err != nil {
					slog.Error("load config", "err", err)
					continue
				}
				if newInjects == nil {
					proxy.SetInjects(&fault.Injects{})
				} else {
					proxy.SetInjects(newInjects)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return proxy, nil
}
