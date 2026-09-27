package fault

import (
	"bytes"
	"testing"
)

func TestCorrupt_MutateTriggered(t *testing.T) {
	msg := []byte("0123456789abcdef")
	want := bytes.Clone(msg)
	c := NewCorrupt(4)
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

// An empty message has no byte to pick, and picking one anyway would panic.
func TestCorrupt_MutateEmptyMessage(t *testing.T) {
	c := NewCorrupt(4)
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
	c := NewCorrupt(1)
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
// and the same byte can be picked twice. TestCorrupt_MutateRepeatedIndex pins
// that down with rolls of its own; here it only has to hold.
func TestCorrupt_MutateDamagesAtMostCount(t *testing.T) {
	c := NewCorrupt(3)
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

// With the rolls fixed, count stops being an upper bound and becomes the
// answer: three distinct indexes, three damaged bytes, and each one exactly
// where the dice said. The masks are the second roll of each pair.
func TestCorrupt_MutateDamagesTheBytesTheDicePicks(t *testing.T) {
	msg := []byte("0123456789abcdef")
	want := bytes.Clone(msg)
	c := Corrupt{
		count: 3,
		dice:  &sequenceDice{t: t, ints: []int{1, 0, 7, 0, 13, 0}},
	}

	out, _, err := c.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countDiff(want, out); n != 3 {
		t.Fatalf("expected exactly 3 damaged bytes, got %d", n)
	}
	for _, i := range []int{1, 7, 13} {
		if out[i] == want[i] {
			t.Errorf("byte %d was picked and left unchanged", i)
		}
	}
}

// Why count is an upper bound: the same index drawn twice is damaged twice, and
// the message ends up one byte short of what count promised. The two masks
// differ here, so the second pass moves the byte further rather than putting it
// back -- with one mask twice, XOR would undo itself and leave the message
// whole.
func TestCorrupt_MutateRepeatedIndex(t *testing.T) {
	msg := []byte("0123456789abcdef")
	want := bytes.Clone(msg)
	c := Corrupt{
		count: 2,
		dice:  &sequenceDice{t: t, ints: []int{4, 0, 4, 1}},
	}

	out, _, err := c.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countDiff(want, out); n != 1 {
		t.Fatalf("expected 1 damaged byte from two rolls of the same index, got %d", n)
	}
	if out[4] != want[4]^1^2 {
		t.Errorf("byte 4 = %#x, want both masks applied: %#x", out[4], want[4]^1^2)
	}
}
