package config

import (
	"path/filepath"
	"testing"
)

// examplePath is the shipped configuration, one directory up from this package.
const examplePath = "../tamper.example.yml"

// The example is what a new user copies, so it has to survive the same strict
// parsing and validation as a real configuration.
func TestExampleConfig_Loads(t *testing.T) {
	cfg, err := Load(filepath.FromSlash(examplePath))
	if err != nil {
		t.Fatalf("the shipped example does not load: %v", err)
	}
	if len(cfg.Rules) == 0 {
		t.Fatal("expected the example to carry rules, got none")
	}
}

// Every fault type is shown at least once. Adding a type to this list and
// leaving the example alone fails the test; adding one and forgetting the list
// as well does not, so the list is part of what a new type has to update.
func TestExampleConfig_ShowsEveryFaultType(t *testing.T) {
	cfg, err := Load(filepath.FromSlash(examplePath))
	if err != nil {
		t.Fatalf("the shipped example does not load: %v", err)
	}

	shown := make(map[string]bool, len(cfg.Rules))
	for _, r := range cfg.Rules {
		shown[r.Fault.Type] = true
	}

	for _, want := range []string{TypeDelay, TypeAbort, TypeTruncate, TypeCorrupt, TypeDrop} {
		if !shown[want] {
			t.Errorf("fault type %q is missing from the example", want)
		}
	}
}
