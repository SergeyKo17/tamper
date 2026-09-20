package fault

// Truncate is a mutator that cuts a message down to size bytes, which is what
// a receiver sees when a connection dies mid-message.
type Truncate struct {
	size int
}

// NewTruncate creates a Truncate mutator that cuts messages down to size bytes.
func NewTruncate(size int) Truncate {
	return Truncate{size: size}
}

// Mutate keeps the first size bytes of the message. A message already at or
// below that length passes through untouched: padding it would make the fault
// grow messages instead of cutting them.
func (t Truncate) Mutate(msg []byte) ([]byte, bool, error) {
	if len(msg) <= t.size {
		return msg, true, nil
	}
	return msg[:t.size], true, nil
}
