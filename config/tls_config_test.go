package config

import (
	"strings"
	"testing"
)

func TestLoad_TLSAndLimits(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "listen tls needs a key",
			yaml: `
listen:
  addr: ":9090"
  tls:
    cert_file: "tamper.crt"
target:
  addr: "localhost:50051"
`,
			wantErr: "listen tls needs both cert_file and key_file",
		},
		{
			name: "listen tls needs a certificate",
			yaml: `
listen:
  addr: ":9090"
  tls:
    key_file: "tamper.key"
target:
  addr: "localhost:50051"
`,
			wantErr: "listen tls needs both cert_file and key_file",
		},
		{
			name: "target tls rejects half a client certificate",
			yaml: `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
  tls:
    ca_file: "ca.crt"
    cert_file: "client.crt"
`,
			wantErr: "target tls needs cert_file and key_file together",
		},
		{
			name: "negative listen limit",
			yaml: `
listen:
  addr: ":9090"
  max_recv_msg_size: -1
target:
  addr: "localhost:50051"
`,
			wantErr: "listen message size limits must not be negative",
		},
		{
			name: "negative target limit",
			yaml: `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
  max_send_msg_size: -1
`,
			wantErr: "target message size limits must not be negative",
		},
		{
			name: "complete tls on both sides",
			yaml: `
listen:
  addr: ":9090"
  tls:
    cert_file: "tamper.crt"
    key_file: "tamper.key"
target:
  addr: "localhost:50051"
  tls:
    ca_file: "ca.crt"
    server_name: "api.example.com"
    cert_file: "client.crt"
    key_file: "client.key"
`,
		},
		{
			name: "target tls with a trust anchor alone",
			yaml: `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
  tls:
    ca_file: "ca.crt"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tmpConfigFile(t, tt.yaml))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestLoad_TLSBlocksAreOptional(t *testing.T) {
	yaml := `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
`
	cfg, err := Load(tmpConfigFile(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen.TLS != nil {
		t.Errorf("expected no listen TLS, got %+v", cfg.Listen.TLS)
	}
	if cfg.Target.TLS != nil {
		t.Errorf("expected no target TLS, got %+v", cfg.Target.TLS)
	}
}

func TestLoad_ParsesTLSFields(t *testing.T) {
	yaml := `
listen:
  addr: ":9090"
  max_recv_msg_size: 1048576
target:
  addr: "localhost:50051"
  tls:
    ca_file: "ca.crt"
    server_name: "api.example.com"
    insecure_skip_verify: true
`
	cfg, err := Load(tmpConfigFile(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen.MaxRecvMsgSize != 1048576 {
		t.Errorf("max_recv_msg_size = %d, want 1048576", cfg.Listen.MaxRecvMsgSize)
	}
	if cfg.Target.TLS.CAFile != "ca.crt" {
		t.Errorf("ca_file = %q, want %q", cfg.Target.TLS.CAFile, "ca.crt")
	}
	if cfg.Target.TLS.ServerName != "api.example.com" {
		t.Errorf("server_name = %q, want %q", cfg.Target.TLS.ServerName, "api.example.com")
	}
	if !cfg.Target.TLS.InsecureSkipVerify {
		t.Error("insecure_skip_verify = false, want true")
	}
}
