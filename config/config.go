package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// TypeDelay is a fault that adds latency before forwarding the request.
	TypeDelay = "delay"
	// TypeAbort is a fault that immediately returns a gRPC error.
	TypeAbort = "abort"
)

// Config holds the proxy configuration loaded from a YAML file.
type Config struct {
	Listen Listen `yaml:"listen"`
	Target Target `yaml:"target"`
	Rules  []Rule `yaml:"rules"`
	Log    Logger `yaml:"logger"`
}

// Listen describes the side of the proxy that faces gRPC clients.
type Listen struct {
	Addr string `yaml:"addr"`
	// TLS terminates incoming connections. A missing block serves plaintext.
	TLS *ServerTLS `yaml:"tls"`
	// MaxRecvMsgSize and MaxSendMsgSize cap message sizes. Zero means the proxy
	// imposes no limit of its own and leaves the peers to enforce theirs.
	MaxRecvMsgSize int `yaml:"max_recv_msg_size"`
	MaxSendMsgSize int `yaml:"max_send_msg_size"`
}

// Target describes the side of the proxy that faces the upstream gRPC server.
type Target struct {
	Addr string `yaml:"addr"`
	// TLS secures the connection to the target. A missing block dials plaintext.
	TLS *ClientTLS `yaml:"tls"`
	// MaxRecvMsgSize and MaxSendMsgSize cap message sizes. Zero means the proxy
	// imposes no limit of its own and leaves the peers to enforce theirs.
	MaxRecvMsgSize int `yaml:"max_recv_msg_size"`
	MaxSendMsgSize int `yaml:"max_send_msg_size"`
}

// ServerTLS holds the certificate the proxy presents to gRPC clients.
type ServerTLS struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// ClientTLS configures how the proxy authenticates the target.
type ClientTLS struct {
	// CAFile is the trust anchor used to verify the target certificate. When
	// empty the system roots are used.
	CAFile string `yaml:"ca_file"`
	// ServerName overrides the name checked against the target certificate,
	// which is needed when the target is dialed by IP.
	ServerName string `yaml:"server_name"`
	// InsecureSkipVerify disables verification of the target certificate.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
	// CertFile and KeyFile hold the client certificate for targets that
	// require mutual TLS.
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Rule maps a request matcher to a fault that should be injected.
type Rule struct {
	Match Match `yaml:"match"`
	Fault Fault `yaml:"fault"`
}

// Match defines criteria for selecting which gRPC requests to intercept.
type Match struct {
	Method string `yaml:"method"`
}

// Fault describes the failure to inject: its type, probability, and parameters.
type Fault struct {
	Type        string        `yaml:"type"`
	Code        int           `yaml:"code"`
	Message     string        `yaml:"msg"`
	Probability float64       `yaml:"prob"`
	Duration    time.Duration `yaml:"duration"`
}

// Logger holds logger configs.
type Logger struct {
	Level string `yaml:"level"`
}

// Load reads and validates a YAML configuration file at the given path.
func Load(path string) (*Config, error) {
	//nolint:gosec
	configFile, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(configFile, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}

	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	if err := validateMatch(cfg.Rules); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func validateConfig(cfg Config) error {
	if err := validateListen(cfg.Listen); err != nil {
		return err
	}
	if err := validateTarget(cfg.Target); err != nil {
		return err
	}
	return validateRules(cfg.Rules)
}

func validateListen(l Listen) error {
	if l.Addr == "" {
		return errors.New("listen address is empty")
	}
	if l.MaxRecvMsgSize < 0 || l.MaxSendMsgSize < 0 {
		return errors.New("listen message size limits must not be negative")
	}
	if l.TLS == nil {
		return nil
	}
	if l.TLS.CertFile == "" || l.TLS.KeyFile == "" {
		return errors.New("listen tls needs both cert_file and key_file")
	}
	return nil
}

func validateTarget(t Target) error {
	if t.Addr == "" {
		return errors.New("target address is empty")
	}
	if t.MaxRecvMsgSize < 0 || t.MaxSendMsgSize < 0 {
		return errors.New("target message size limits must not be negative")
	}
	if t.TLS == nil {
		return nil
	}
	// A client certificate is optional, but half of one is a mistake.
	if (t.TLS.CertFile == "") != (t.TLS.KeyFile == "") {
		return errors.New("target tls needs cert_file and key_file together")
	}
	return nil
}

func validateRules(rules []Rule) error {
	for _, r := range rules {
		if r.Fault.Type != TypeDelay && r.Fault.Type != TypeAbort {
			return errors.New("unsupported fault type: " + r.Fault.Type)
		}
		if r.Fault.Probability < 0 || r.Fault.Probability > 1 {
			return fmt.Errorf("fault probability must be between 0 and 1, got: %f", r.Fault.Probability)
		}
		if r.Fault.Type == TypeAbort && (r.Fault.Code < 1 || r.Fault.Code > 16) {
			return fmt.Errorf("fault code must be between 1 and 16, got: %d", r.Fault.Code)
		}
	}
	return nil
}

func validateMatch(rules []Rule) error {
	for _, r := range rules {
		switch r.Match.Method {
		case "*":
			continue
		case "":
			return errors.New("method shouldn't be empty")
		}
		if r.Match.Method[0] != byte('/') {
			return errors.New("pattern should start with /")
		}
		if _, err := path.Match(r.Match.Method, ""); err != nil {
			return fmt.Errorf("match method %q: %w", r.Match.Method, err)
		}
	}
	return nil
}

// Matches reports whether a gRPC method falls under the rule's pattern. A bare
// "*" selects everything: path.Match stops its wildcard at the separator, so on
// its own it would never span a full "/service/method" path.
func (m *Match) Matches(method string) bool {
	if m.Method == "*" {
		return true
	}
	ok, err := path.Match(m.Method, method)
	return err == nil && ok
}
