package source

// Cursor is the last Source journal sequence consumed by one binding. It is a
// value type because trigger transitions are pure.
type Cursor struct{ sequence int64 }

func NewCursor(sequence int64) Cursor { return Cursor{sequence: sequence} }
func (c Cursor) Sequence() int64      { return c.sequence }
