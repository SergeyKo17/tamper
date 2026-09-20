package fault

import (
	"bytes"
	"testing"
)

func TestDrop_MutateTriggered(t *testing.T) {
	d := NewDrop(1.0)
	_, forward, err := d.Mutate([]byte("payload"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if forward {
		t.Fatal("expected the message to be dropped, got forward=true")
	}
}

func TestDrop_MutateSkipped(t *testing.T) {
	d := NewDrop(0.0)
	msg := []byte("payload")
	out, forward, err := d.Mutate(msg)
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
