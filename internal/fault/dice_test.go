package fault

import "testing"

// sequenceDice is a dice with the randomness taken out: it hands over the
// numbers the test gave it, in order. Asking for one it does not have fails the
// test rather than falling back to anything, so a test that rolls more often
// than it meant to says so instead of passing by luck.
type sequenceDice struct {
	t      *testing.T
	floats []float64
	ints   []int
}

func (d *sequenceDice) Float64() float64 {
	d.t.Helper()
	if len(d.floats) == 0 {
		d.t.Fatal("Float64 rolled more often than the test has numbers for")
		return 0
	}
	next := d.floats[0]
	d.floats = d.floats[1:]
	return next
}

func (d *sequenceDice) IntN(n int) int {
	d.t.Helper()
	if len(d.ints) == 0 {
		d.t.Fatal("IntN rolled more often than the test has numbers for")
		return 0
	}
	next := d.ints[0]
	d.ints = d.ints[1:]
	if next >= n {
		d.t.Fatalf("IntN(%d) cannot return %d", n, next)
		return 0
	}
	return next
}

// A probability between the two certainties decides message by message, and
// which way it goes is the roll against it and nothing else.
func TestFires_FollowsTheRolls(t *testing.T) {
	dice := &sequenceDice{t: t, floats: []float64{0.1, 0.5, 0.2, 0.9}}
	rule := Inject{Probability: 0.3, dice: dice}

	want := []bool{true, false, true, false}
	for i, w := range want {
		if got := rule.Fires(); got != w {
			t.Errorf("roll %d: fired = %v, want %v", i, got, w)
		}
	}
}

// The comparison is strict: a roll landing exactly on the probability does not
// fire. The distinction matters at the ends -- a rule at 0.0 has to stay quiet,
// and Float64 does return 0.0 -- and nothing else pins it down.
func TestFires_RollEqualToProbabilityDoesNotFire(t *testing.T) {
	cases := []struct {
		name        string
		probability float64
		roll        float64
	}{
		{name: "mid range", probability: 0.3, roll: 0.3},
		{name: "zero", probability: 0, roll: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rule := Inject{
				Probability: c.probability,
				dice:        &sequenceDice{t: t, floats: []float64{c.roll}},
			}
			if rule.Fires() {
				t.Errorf("a roll of %v fired against probability %v", c.roll, c.probability)
			}
		})
	}
}

// Both kinds of rule roll their own dice, and a message rule is the one that
// rolls per message rather than once per call.
func TestFires_MessageRuleFollowsTheRolls(t *testing.T) {
	dice := &sequenceDice{t: t, floats: []float64{0.9, 0.1}}
	rule := MessageInject{Probability: 0.5, dice: dice}

	if rule.Fires() {
		t.Error("a roll of 0.9 fired against probability 0.5")
	}
	if !rule.Fires() {
		t.Error("a roll of 0.1 did not fire against probability 0.5")
	}
}
