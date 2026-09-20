package fault

// Drop is a mutator that discards a message instead of relaying it, leaving a
// hole in the stream without an error on either side.
type Drop struct {
	chance
}

// NewDrop creates a Drop mutator that discards messages with the given
// probability.
func NewDrop(probability float64) Drop {
	return Drop{chance: chance(probability)}
}

// Mutate reports that the message should not be forwarded when the fault fires.
func (d Drop) Mutate(msg []byte) ([]byte, bool, error) {
	if !d.fires() {
		return msg, true, nil
	}
	return nil, false, nil
}
