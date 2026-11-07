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
				old := cfg
				cfg = watcher.Config()
				setLevel(lvl, cfg.Log.Level)
				warnRestartNeeded(old, cfg)
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

// warnRestartNeeded reports the fields a reload leaves alone. Only the rules
// and the log level are swapped in place, so a changed listener or target
// would otherwise be announced as a reload and quietly ignored. Fields are
// named as the YAML keys, since that is what was edited.
func warnRestartNeeded(old, cur *config.Config) {
	fields := make([]string, 0, 8)

	if old.Listen.Addr != cur.Listen.Addr {
		fields = append(fields, "listen.addr")
	}
	if old.Listen.MaxRecvMsgSize != cur.Listen.MaxRecvMsgSize {
		fields = append(fields, "listen.max_recv_msg_size")
	}
	if old.Listen.MaxSendMsgSize != cur.Listen.MaxSendMsgSize {
		fields = append(fields, "listen.max_send_msg_size")
	}
	if !samePtr(old.Listen.TLS, cur.Listen.TLS) {
		fields = append(fields, "listen.tls")
	}

	if old.Target.Addr != cur.Target.Addr {
		fields = append(fields, "target.addr")
	}
	if old.Target.MaxRecvMsgSize != cur.Target.MaxRecvMsgSize {
		fields = append(fields, "target.max_recv_msg_size")
	}
	if old.Target.MaxSendMsgSize != cur.Target.MaxSendMsgSize {
		fields = append(fields, "target.max_send_msg_size")
	}
	if !samePtr(old.Target.TLS, cur.Target.TLS) {
		fields = append(fields, "target.tls")
	}

	if len(fields) > 0 {
		slog.Warn("these changes need a restart", "fields", fields)
	}
}

// samePtr compares two optional blocks by value. A reload always brings a
// fresh pointer, so comparing the pointers themselves would report a change
// the file does not have.
func samePtr[T comparable](old, cur *T) bool {
	if old == nil || cur == nil {
		return old == cur
	}
	return *old == *cur
}
