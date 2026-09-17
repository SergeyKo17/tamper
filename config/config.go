package config

import (
	"errors"
	"fmt"
	"os"
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
	Listen string `yaml:"listen"`
	Target string `yaml:"target"`
	Rules  []Rule `yaml:"rules"`
	Log    Logger `yaml:"logger"`
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

	return &cfg, nil
}

func validateConfig(cfg Config) error {
	if cfg.Listen == "" {
		return errors.New("config listener is empty")
	}
	if cfg.Target == "" {
		return errors.New("config target is empty")
	}
	for _, r := range cfg.Rules {
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
