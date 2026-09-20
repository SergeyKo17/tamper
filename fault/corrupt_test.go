package fault

import (
	"bytes"
	"testing"
)

func TestCorrupt_MutateTriggered(t *testing.T) {
	msg := []byte("0123456789abcdef")
	want := bytes.Clone(msg)
	c := NewCorrupt(4, 1.0)
	out, forward, err := c.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !forward {
		t.Fatal("expected the message to be forwarded, got forward=false")
	}
	if bytes.Equal(out, want) {
		t.Fatal("expected the message to be damaged, got it unchanged")
	}
	if len(out) != len(want) {
		t.Fatalf("expected length %d, got %d", len(want), len(out))
	}
}

func TestCorrupt_MutateSkipped(t *testing.T) {
	msg := []byte("0123456789abcdef")
	want := bytes.Clone(msg)
	c := NewCorrupt(4, 0.0)
	out, forward, err := c.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !forward {
		t.Fatal("expected the message to be forwarded, got forward=false")
	}
	if !bytes.Equal(out, want) {
		t.Fatalf("expected the message untouched, got %q", out)
	}
}

// An empty message has no byte to pick, and picking one anyway would panic.
func TestCorrupt_MutateEmptyMessage(t *testing.T) {
	c := NewCorrupt(4, 1.0)
	out, forward, err := c.Mutate([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !forward {
		t.Fatal("expected the message to be forwarded, got forward=false")
	}
	if len(out) != 0 {
		t.Fatalf("expected an empty message, got %q", out)
	}
}

// The XOR mask is never zero, so a single pass over a single index always
// leaves exactly one byte different.
func TestCorrupt_MutateAlwaysChangesAByte(t *testing.T) {
	c := NewCorrupt(1, 1.0)
	for i := range 100 {
		msg := []byte("0123456789abcdef")
		want := bytes.Clone(msg)
		out, _, err := c.Mutate(msg)
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
		if n := countDiff(want, out); n != 1 {
			t.Fatalf("run %d: expected exactly 1 damaged byte, got %d", i, n)
		}
	}
}

// count is an upper bound rather than a guarantee: indexes are drawn at random
// and the same byte can be picked twice.
func TestCorrupt_MutateDamagesAtMostCount(t *testing.T) {
	c := NewCorrupt(3, 1.0)
	for i := range 100 {
		msg := bytes.Repeat([]byte("x"), 64)
		want := bytes.Clone(msg)
		out, _, err := c.Mutate(msg)
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
		if n := countDiff(want, out); n > 3 {
			t.Fatalf("run %d: expected at most 3 damaged bytes, got %d", i, n)
		}
	}
}

func countDiff(a, b []byte) int {
	var n int
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}
