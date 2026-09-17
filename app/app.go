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
	watcher, err := config.Watch(ctx, path)
	if err != nil {
		return nil, err
	}
	cfg := watcher.Config()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	injectors, err := fault.NewInjectors(cfg.Rules)
	if err != nil {
		return nil, err
	}

	proxy, err := proxy.New(ctx, cfg.Listen, cfg.Target, injectors)
	if err != nil {
		return nil, err
	}

	return proxy, nil
}
