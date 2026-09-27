package fault

import "slices"

// Corrupt is a mutator that replaces count bytes of a message with random
// values, leaving its length untouched. Decoding such a message usually fails
// outright; when it does not, a field quietly carries a different value.
type Corrupt struct {
	count int
	// dice picks the bytes and the masks. A zero value rolls the global one.
	dice roller
}

// NewCorrupt creates a Corrupt mutator that damages count bytes.
func NewCorrupt(count int) Corrupt {
	return Corrupt{count: count}
}

// Mutate damages count distinct bytes, drawn at random, and a count larger than
// the message means every byte of it. The XOR mask is never zero, so a byte it
// touches is guaranteed to change, and no two draws land on the same byte to
// change it back.
func (c Corrupt) Mutate(msg []byte) ([]byte, bool, error) {
	if len(msg) == 0 {
		return msg, true, nil
	}
	n := min(c.count, len(msg))

	d := roll(c.dice)
	picked := make([]int, 0, n)
	for len(picked) < n {
		i := d.IntN(len(msg))
		if slices.Contains(picked, i) {
			continue
		}
		picked = append(picked, i)
		//nolint:gosec // IntN(255) tops out at 254, so the mask fits a byte.
		msg[i] ^= byte(1 + d.IntN(255))
	}
	return msg, true, nil
}
