package fault

import "math/rand/v2"

// roller is the source of randomness a rule rolls to decide whether it fires
// and, for corrupt, which bytes it damages. It is a field rather than a direct
// call into math/rand so that a test can hand over a known sequence: rand/v2
// seeds its global generator itself and offers no way to steer it.
type roller interface {
	Float64() float64
	IntN(n int) int
}

// defaultDice is what a rule rolls when it was given no dice of its own.
var defaultDice roller = globalDice{}

// roll resolves the dice to use. Nothing but a test passes one explicitly, so
// everything built as a literal arrives here with none.
func roll(d roller) roller {
	if d == nil {
		return defaultDice
	}
	return d
}

// globalDice is the production source, and the only implementation outside
// tests: the generator built into rand/v2.
type globalDice struct{}

func (globalDice) Float64() float64 {
	return rand.Float64() //nolint:gosec
}

func (globalDice) IntN(n int) int {
	return rand.IntN(n) //nolint:gosec
}
