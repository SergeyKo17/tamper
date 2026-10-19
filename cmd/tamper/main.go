package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/SergeyKo17/tamper/app"
	"github.com/SergeyKo17/tamper/config"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	if err := run(); err != nil {
		slog.Error("tamper failed", "version", version, "commit", commit, "err", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "./tamper.yml", "config path")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("tamper %s (%s)\n", version, commit)
		return nil
	}

	// The logger belongs to the process, not to the proxy: everything below is
	// handed the level variable this handler reads, and sets it from the
	// configuration without replacing the handler.
	var lvl slog.LevelVar
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &lvl})))

	// Notified before the proxy is assembled, so a startup that waits on an
	// unreachable target can still be interrupted.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	proxy, watcher, err := app.New(ctx, *configPath, &lvl)
	if err != nil {
		return err
	}
	go reloadOnHUP(ctx, hup, watcher)

	slog.Info("tamper started",
		"version", version,
		"commit", commit,
		"listen", proxy.Addr().String(),
		"target", watcher.Config().Target.Addr,
	)

	return proxy.Run(ctx)
}

// reloadOnHUP rereads the configuration on every SIGHUP. It is the reload path
// that does not depend on the file system reporting the change: a config
// bind-mounted from a Windows host delivers no events to the container.
func reloadOnHUP(ctx context.Context, hup <-chan os.Signal, watcher *config.Watcher) {
	for {
		select {
		case <-hup:
			if err := watcher.Reload(); err != nil {
				slog.Error("reload config", "err", err)
				continue
			}
			slog.Info("configuration reloaded")
		case <-ctx.Done():
			return
		}
	}
}
