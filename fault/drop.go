package fault

// Drop is a mutator that discards a message instead of relaying it, leaving a
// hole in the stream without an error on either side.
type Drop struct{}

// NewDrop creates a Drop mutator that discards messages.
func NewDrop() Drop {
	return Drop{}
}

// Mutate reports that the message should not be forwarded.
func (d Drop) Mutate(_ []byte) ([]byte, bool, error) {
	return nil, false, nil
}
