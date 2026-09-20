package fault

import "testing"

func TestDrop_MutateTriggered(t *testing.T) {
	d := NewDrop()
	_, forward, err := d.Mutate([]byte("payload"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if forward {
		t.Fatal("expected the message to be dropped, got forward=true")
	}
}
