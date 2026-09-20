package fault

import "math/rand/v2"

// Corrupt is a mutator that replaces count bytes of a message with random
// values, leaving its length untouched. Decoding such a message usually fails
// outright; when it does not, a field quietly carries a different value.
type Corrupt struct {
	count int
}

// NewCorrupt creates a Corrupt mutator that damages count bytes.
func NewCorrupt(count int) Corrupt {
	return Corrupt{count: count}
}

// Mutate damages count randomly chosen bytes. The XOR mask is never zero, so a
// byte it touches is guaranteed to change.
func (c Corrupt) Mutate(msg []byte) ([]byte, bool, error) {
	if len(msg) == 0 {
		return msg, true, nil
	}
	for range c.count {
		i := rand.IntN(len(msg))           //nolint:gosec
		msg[i] ^= byte(1 + rand.IntN(255)) //nolint:gosec
	}
	return msg, true, nil
}
