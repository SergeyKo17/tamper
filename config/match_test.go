package config

import "testing"

func TestMatch_Matches(t *testing.T) {
	const method = "/pkg.Service/Method"

	cases := []struct {
		name    string
		pattern string
		want    bool
	}{
		{name: "exact", pattern: method, want: true},
		{name: "exact other method", pattern: "/pkg.Service/Other", want: false},
		{name: "service wildcard", pattern: "/pkg.Service/*", want: true},
		{name: "service wildcard elsewhere", pattern: "/other.Service/*", want: false},
		{name: "method wildcard", pattern: "/*/Method", want: true},
		{name: "method wildcard other name", pattern: "/*/Other", want: false},
		{name: "both segments", pattern: "/*/*", want: true},
		{name: "everything", pattern: "*", want: true},
		// A wildcard stops at the separator, so a single segment cannot stand
		// for a whole method path. That is why "*" is special-cased above.
		{name: "single segment", pattern: "/*", want: false},
		{name: "empty", pattern: "", want: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Match{Method: c.pattern}
			if got := m.Matches(method); got != c.want {
				t.Errorf("Match{%q}.Matches(%q) = %v, want %v", c.pattern, method, got, c.want)
			}
		})
	}
}

func TestNew_MethodPatterns(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		wantErr bool
	}{
		{name: "exact", method: "/pkg.Service/Method"},
		{name: "service wildcard", method: "/pkg.Service/*"},
		{name: "everything", method: "*"},
		{name: "empty", method: "", wantErr: true},
		{name: "no leading slash", method: "pkg.Service/*", wantErr: true},
		{name: "malformed", method: "/pkg.[Service/*", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yaml := `
listen:
  addr: ":9090"
target:
  addr: "localhost:50051"
rules:
  - match:
      method: "` + c.method + `"
    fault:
      type: delay
      prob: 0.5
      duration: 500ms
`
			_, err := Load(tmpConfigFile(t, yaml))
			if c.wantErr && err == nil {
				t.Fatalf("method %q: expected a validation error, got nil", c.method)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("method %q: unexpected error: %v", c.method, err)
			}
		})
	}
}
