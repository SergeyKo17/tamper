package fault

import "math/rand/v2"

// Corrupt is a mutator that replaces count bytes of a message with random
// values, leaving its length untouched. Decoding such a message usually fails
// outright; when it does not, a field quietly carries a different value.
type Corrupt struct {
	chance
	count int
}

// NewCorrupt creates a Corrupt mutator that damages count bytes with the given
// probability.
func NewCorrupt(count int, probability float64) Corrupt {
	return Corrupt{chance: chance(probability), count: count}
}

// Mutate damages count randomly chosen bytes based on probability. The XOR mask
// is never zero, so a byte it touches is guaranteed to change.
func (c Corrupt) Mutate(msg []byte) ([]byte, bool, error) {
	if !c.fires() || len(msg) == 0 {
		return msg, true, nil
	}
	for range c.count {
		i := rand.IntN(len(msg))           //nolint:gosec
		msg[i] ^= byte(1 + rand.IntN(255)) //nolint:gosec
	}
	return msg, true, nil
}
