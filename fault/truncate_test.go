package fault

import (
	"bytes"
	"testing"
)

func TestTruncate_MutateTriggered(t *testing.T) {
	msg := []byte("0123456789")
	tr := NewTruncate(4, 1.0)
	out, forward, err := tr.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !forward {
		t.Fatal("expected the message to be forwarded, got forward=false")
	}
	if !bytes.Equal(out, []byte("0123")) {
		t.Fatalf("expected the first 4 bytes, got %q", out)
	}
}

func TestTruncate_MutateSkipped(t *testing.T) {
	msg := []byte("0123456789")
	tr := NewTruncate(4, 0.0)
	out, _, err := tr.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, msg) {
		t.Fatalf("expected the message untouched, got %q", out)
	}
}

// A message already short enough is passed on as it is: the fault cuts messages
// down, it never pads them out to size.
func TestTruncate_MutateShorterThanSize(t *testing.T) {
	msg := []byte("012")
	tr := NewTruncate(8, 1.0)
	out, forward, err := tr.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !forward {
		t.Fatal("expected the message to be forwarded, got forward=false")
	}
	if !bytes.Equal(out, msg) {
		t.Fatalf("expected the message untouched, got %q", out)
	}
}

func TestTruncate_MutateExactlySize(t *testing.T) {
	msg := []byte("0123")
	tr := NewTruncate(4, 1.0)
	out, _, err := tr.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, msg) {
		t.Fatalf("expected the message untouched, got %q", out)
	}
}

// Truncating to nothing still forwards: an empty message on the wire is a
// different fault from no message at all, which is what Drop does.
func TestTruncate_MutateToEmpty(t *testing.T) {
	tr := NewTruncate(0, 1.0)
	out, forward, err := tr.Mutate([]byte("0123456789"))
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

// What travels is the slice, so only its length decides how much is sent. The
// cut-off tail stays in the backing array and must stay out of reach.
func TestTruncate_MutateDoesNotExposeTail(t *testing.T) {
	msg := []byte("0123456789")
	tr := NewTruncate(4, 1.0)
	out, _, err := tr.Mutate(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("expected length 4, got %d", len(out))
	}
}
