package source

import "fmt"

// Cursor is the last Source journal sequence consumed by one binding. It is a
// value type because trigger transitions are pure.
type Cursor struct{ sequence int64 }

func NewCursor(sequence int64) Cursor { return Cursor{sequence: sequence} }
func (c Cursor) Sequence() int64      { return c.sequence }

// JournalEntry is one resolved Source in stable per-scope insertion order.
// The Source value is immutable by contract; concrete built-ins copy mutable
// fields at their access boundaries.
type JournalEntry struct {
	ref      Ref
	value    Value
	position int64
}

func NewJournalEntry(ref Ref, value Value, position int64) (JournalEntry, error) {
	if _, err := NewRef(ref.Type(), ref.ID()); err != nil {
		return JournalEntry{}, err
	}
	if err := validateValue(value); err != nil {
		return JournalEntry{}, err
	}
	if value.SourceName() != ref.ID() {
		return JournalEntry{}, fmt.Errorf("Source journal identity does not match its value")
	}
	if position < 1 {
		return JournalEntry{}, fmt.Errorf("Source journal position must be positive")
	}
	return JournalEntry{ref: ref, value: value, position: position}, nil
}

func (e JournalEntry) Ref() Ref        { return e.ref }
func (e JournalEntry) Value() Value    { return e.value }
func (e JournalEntry) Position() int64 { return e.position }
