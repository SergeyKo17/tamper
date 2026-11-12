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

// count is a guarantee, not an upper bound: a repeated index is redrawn, so the
// count is reached however the draws fall.
func TestCorrupt_MutateDamagesExactlyCount(t *testing.T) {
	c := NewCorrupt(3)
	for i := range 100 {
		msg := bytes.Repeat([]byte("x"), 64)
		want := bytes.Clone(msg)
		out, _, err := c.Mutate(msg)
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
		if n := countDiff(want, out); n != 3 {
			t.Fatalf("run %d: expected exactly 3 damaged bytes, got %d", i, n)
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

// An index drawn twice is not damaged twice: the repeat is dropped and redrawn,
// which costs a roll and still leaves count bytes damaged. The rolls here are
// index, mask, the repeat, then the index that replaces it and its mask.
func TestCorrupt_MutateRedrawsARepeatedIndex(t *testing.T) {
	msg := []byte("0123456789abcdef")
	want := bytes.Clone(msg)
	c := Corrupt{
		count: 2,
		dice:  &sequenceDice{t: t, ints: []int{4, 0, 4, 9, 1}},
	}

	out, _, err := c.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countDiff(want, out); n != 2 {
		t.Fatalf("expected 2 damaged bytes, got %d", n)
	}
	if out[4] != want[4]^1 {
		t.Errorf("byte 4 = %#x, want the first mask only: %#x", out[4], want[4]^1)
	}
	if out[9] != want[9]^2 {
		t.Errorf("byte 9 = %#x, want the second mask: %#x", out[9], want[9]^2)
	}
}

// A count past the end of the message means the whole message. Without the cap
// the draw for a distinct index it cannot have would never finish.
func TestCorrupt_MutateCountLargerThanMessage(t *testing.T) {
	msg := []byte("0123")
	want := bytes.Clone(msg)
	c := NewCorrupt(10)

	out, _, err := c.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countDiff(want, out); n != len(want) {
		t.Fatalf("expected all %d bytes damaged, got %d", len(want), n)
	}
}
