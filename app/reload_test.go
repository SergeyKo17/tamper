package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"

	"github.com/SergeyKo17/tamper/config"
)

// captureWarn redirects the default logger into a buffer for the length of the
// test, which is where warnRestartNeeded reports.
func captureWarn(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// baseConfig is a configuration with both TLS blocks present, so a test can
// rebuild it and get the same values behind different pointers.
func baseConfig() *config.Config {
	return &config.Config{
		Listen: config.Listen{
			Addr: "127.0.0.1:9090",
			TLS:  &config.ServerTLS{CertFile: "certs/tamper.crt", KeyFile: "certs/tamper.key"},
		},
		Target: config.Target{
			Addr: "127.0.0.1:50051",
			TLS:  &config.ClientTLS{CAFile: "certs/upstream-ca.crt"},
		},
		Log: config.Logger{Level: "info"},
	}
}

func TestWarnRestartNeeded(t *testing.T) {
	tests := []struct {
		name string
		edit func(*config.Config)
		want []string
	}{
		{
			name: "a reload of the same file changes nothing",
			edit: func(*config.Config) {},
		},
		{
			name: "the rules and the level are applied in place",
			edit: func(cfg *config.Config) {
				cfg.Log.Level = "debug"
				cfg.Rules = []config.Rule{{Match: config.Match{Method: "*"}}}
			},
		},
		{
			name: "the listen address",
			edit: func(cfg *config.Config) { cfg.Listen.Addr = "127.0.0.1:9091" },
			want: []string{"listen.addr"},
		},
		{
			name: "message limits on the listen side",
			edit: func(cfg *config.Config) {
				cfg.Listen.MaxRecvMsgSize = 1 << 20
				cfg.Listen.MaxSendMsgSize = 1 << 20
			},
			want: []string{"listen.max_recv_msg_size", "listen.max_send_msg_size"},
		},
		{
			name: "a rewritten listen certificate",
			edit: func(cfg *config.Config) { cfg.Listen.TLS.CertFile = "certs/other.crt" },
			want: []string{"listen.tls"},
		},
		{
			name: "the target moved and lost its tls block",
			edit: func(cfg *config.Config) {
				cfg.Target.Addr = "10.0.0.2:50051"
				cfg.Target.TLS = nil
			},
			want: []string{"target.addr", "target.tls"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := captureWarn(t)
			old, cur := baseConfig(), baseConfig()
			tt.edit(cur)

			warnRestartNeeded(old, cur)

			if tt.want == nil {
				if buf.Len() != 0 {
					t.Fatalf("expected no warning, got %s", buf)
				}
				return
			}

			var record struct {
				Msg    string   `json:"msg"`
				Fields []string `json:"fields"`
			}
			if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
				t.Fatalf("decode log record %q: %v", buf, err)
			}
			if record.Msg != "these changes need a restart" {
				t.Errorf("msg = %q", record.Msg)
			}
			if !slices.Equal(record.Fields, tt.want) {
				t.Errorf("fields = %v, want %v", record.Fields, tt.want)
			}
		})
	}
}
