package config

import "testing"

// loadFault builds a config around a single fault block and loads it. The block
// is written as it would appear under "fault:", indented six spaces.
func loadFault(t *testing.T, fault string) (*Config, error) {
	t.Helper()
	yaml := `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
rules:
  - match:
      method: "/pkg.Service/Method"
    fault:
` + fault
	return Load(tmpConfigFile(t, yaml))
}

func TestNew_FaultValidation(t *testing.T) {
	cases := []struct {
		name    string
		fault   string
		wantErr bool
	}{
		{
			name:  "delay",
			fault: "      type: delay\n      prob: 0.5\n      duration: 500ms\n",
		},
		{
			name:    "delay without duration",
			fault:   "      type: delay\n      prob: 0.5\n",
			wantErr: true,
		},
		{
			name:    "delay with direction",
			fault:   "      type: delay\n      prob: 0.5\n      duration: 500ms\n      direction: request\n",
			wantErr: true,
		},
		{
			name:  "abort",
			fault: "      type: abort\n      prob: 1.0\n      code: 14\n",
		},
		{
			name:    "abort without code",
			fault:   "      type: abort\n      prob: 1.0\n",
			wantErr: true,
		},
		{
			name:  "truncate",
			fault: "      type: truncate\n      prob: 1.0\n      size: 128\n      direction: response\n",
		},
		{
			name:    "truncate without size",
			fault:   "      type: truncate\n      prob: 1.0\n",
			wantErr: true,
		},
		{
			name:    "truncate with unknown direction",
			fault:   "      type: truncate\n      prob: 1.0\n      size: 128\n      direction: inbound\n",
			wantErr: true,
		},
		{
			name:  "corrupt takes a default count",
			fault: "      type: corrupt\n      prob: 0.1\n",
		},
		{
			name:    "corrupt with negative count",
			fault:   "      type: corrupt\n      prob: 0.1\n      count: -1\n",
			wantErr: true,
		},
		{
			name:  "drop in both directions",
			fault: "      type: drop\n      prob: 0.2\n      direction: both\n",
		},
		{
			name:    "unknown type",
			fault:   "      type: mangle\n      prob: 0.2\n",
			wantErr: true,
		},
		{
			name:    "corrupt sized by size",
			fault:   "      type: corrupt\n      prob: 0.1\n      size: 100\n",
			wantErr: true,
		},
		{
			name:    "truncate counted by count",
			fault:   "      type: truncate\n      prob: 0.1\n      size: 128\n      count: 4\n",
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadFault(t, c.fault)
			if c.wantErr && err == nil {
				t.Fatal("expected a validation error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNew_MessageFaultDefaults(t *testing.T) {
	cases := []struct {
		name          string
		fault         string
		wantCount     int
		wantDirection string
	}{
		{
			name:          "corrupt",
			fault:         "      type: corrupt\n      prob: 0.1\n",
			wantCount:     1,
			wantDirection: DirectionRequest,
		},
		{
			name:          "explicit values are kept",
			fault:         "      type: corrupt\n      prob: 0.1\n      count: 7\n      direction: response\n",
			wantCount:     7,
			wantDirection: DirectionResponse,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := loadFault(t, c.fault)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := cfg.Rules[0].Fault
			if got.Count != c.wantCount {
				t.Errorf("count = %d, want %d", got.Count, c.wantCount)
			}
			if got.Direction != c.wantDirection {
				t.Errorf("direction = %q, want %q", got.Direction, c.wantDirection)
			}
		})
	}
}

// TestNew_UnknownField checks that a mistyped key is rejected rather than
// dropped, which would leave a rule running on zero values.
func TestNew_UnknownField(t *testing.T) {
	if _, err := loadFault(t, "      type: delay\n      prob: 0.5\n      durration: 500ms\n"); err == nil {
		t.Fatal("expected an error for an unknown key in a fault, got nil")
	}

	yaml := `
listen:
  addr: ":9090"
  tls_cert: "server.crt"
target:
  addr: "localhost:50051"
`
	if _, err := Load(tmpConfigFile(t, yaml)); err == nil {
		t.Fatal("expected an error for an unknown key outside rules, got nil")
	}
}
