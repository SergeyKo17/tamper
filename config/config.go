package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
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

	// TypeTruncate is a fault that cuts a message down to Size bytes.
	TypeTruncate = "truncate"
	// TypeCorrupt is a fault that flips Count bytes of a message.
	TypeCorrupt = "corrupt"
	// TypeDrop is a fault that discards a message instead of relaying it.
	TypeDrop = "drop"

	// DirectionRequest applies a message fault on the way to the target.
	DirectionRequest = "request"
	// DirectionResponse applies a message fault on the way back to the client.
	DirectionResponse = "response"
	// DirectionBoth applies a message fault in both directions.
	DirectionBoth = "both"
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
	Name        string        `yaml:"name"`
	Type        string        `yaml:"type"`
	Probability float64       `yaml:"prob"`
	Direction   string        `yaml:"direction"`
	Duration    time.Duration `yaml:"duration"`
	Code        int           `yaml:"code"`
	Message     string        `yaml:"msg"`
	Size        int           `yaml:"size"`
	Count       int           `yaml:"count"`
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
	// Strict decoding: an unknown key is a typo, and a typo that is silently
	// dropped leaves a rule running on zero values.
	dec := yaml.NewDecoder(bytes.NewReader(configFile))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}

	for i := range cfg.Rules {
		applyFaultDefaults(&cfg.Rules[i].Fault)
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
	if err := validateLogger(cfg.Log); err != nil {
		return err
	}
	return validateRules(cfg.Rules)
}

// validateLogger checks that the level is one slog understands. A level it does
// not recognise would leave the proxy running at info while the config says
// otherwise, and a missing debug line is a bad way to learn about a typo.
func validateLogger(l Logger) error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(l.Level)); err != nil {
		return fmt.Errorf("logger level %q: %w", l.Level, err)
	}
	return nil
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
		if err := validateFault(r.Fault); err != nil {
			return err
		}
	}
	return nil
}

func validateFault(f Fault) error {
	if !isKnownFault(f.Type) {
		return errors.New("unsupported fault type: " + f.Type)
	}
	if f.Probability < 0 || f.Probability > 1 {
		return fmt.Errorf("fault probability must be between 0 and 1, got: %f", f.Probability)
	}
	if err := validateDirection(f); err != nil {
		return err
	}
	return validateFaultParams(f)
}

// validateFaultParams checks the settings a fault type carries of its own: a
// delay has a duration, an abort a status code, and a message fault a size or
// a count. Mixing up size and count is rejected rather than defaulted, since
// the rule would otherwise work on a different amount than it says.
func validateFaultParams(f Fault) error {
	switch f.Type {
	case TypeDelay:
		if f.Duration <= 0 {
			return fmt.Errorf("delay duration must be positive, got: %s", f.Duration)
		}
	case TypeAbort:
		if f.Code < 1 || f.Code > 16 {
			return fmt.Errorf("fault code must be between 1 and 16, got: %d", f.Code)
		}
	case TypeTruncate:
		if f.Size <= 0 {
			return fmt.Errorf("truncate size must be positive, got: %d", f.Size)
		}
		if f.Count != 0 {
			return errors.New("truncate is sized in bytes by size, not count")
		}
	case TypeCorrupt:
		if f.Count <= 0 {
			return fmt.Errorf("corrupt count must be positive, got: %d", f.Count)
		}
		if f.Size != 0 {
			return errors.New("corrupt is measured by count, not size")
		}
	}
	return nil
}

// validateDirection checks that a direction is set exactly where it means
// something. A message fault picks the leg it works on; a call-level fault acts
// before the request is forwarded and has no leg to pick.
func validateDirection(f Fault) error {
	if !IsMessageFault(f.Type) {
		if f.Direction != "" {
			return fmt.Errorf("fault type %q takes no direction", f.Type)
		}
		return nil
	}

	switch f.Direction {
	case DirectionRequest, DirectionResponse, DirectionBoth:
		return nil
	default:
		return fmt.Errorf("direction must be %s, %s or %s, got: %q",
			DirectionRequest, DirectionResponse, DirectionBoth, f.Direction)
	}
}

// applyFaultDefaults fills in what a message fault may leave unsaid: the leg it
// works on, and how many bytes it deals with. A zero count would turn the rule
// into a silent no-op, which is worse than a chosen default.
func applyFaultDefaults(f *Fault) {
	if !IsMessageFault(f.Type) {
		return
	}
	if f.Direction == "" {
		f.Direction = DirectionRequest
	}
	if f.Type == TypeCorrupt && f.Count == 0 {
		f.Count = 1
	}
}

// IsMessageFault reports whether a fault works on individual messages rather
// than on the call as a whole.
func IsMessageFault(faultType string) bool {
	switch faultType {
	case TypeTruncate, TypeCorrupt, TypeDrop:
		return true
	}
	return false
}

func isKnownFault(faultType string) bool {
	return faultType == TypeDelay || faultType == TypeAbort || IsMessageFault(faultType)
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
