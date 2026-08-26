package trigger

import (
	"fmt"

	"github.com/ob-labs/powercontext-go/source"
)

const SourceWindowName = "memory-source-window"

type SourceHighWatermark struct {
	sequence int64
	limit    int64
}

func NewSourceHighWatermark(sequence, limit int64) (SourceHighWatermark, error) {
	if sequence < 0 {
		return SourceHighWatermark{}, fmt.Errorf("Source high watermark sequence must be non-negative")
	}
	if limit < 1 {
		return SourceHighWatermark{}, fmt.Errorf("Source window limit must be positive")
	}
	return SourceHighWatermark{sequence: sequence, limit: limit}, nil
}

func (s SourceHighWatermark) Sequence() int64 { return s.sequence }
func (s SourceHighWatermark) Limit() int64    { return s.limit }

type ProcessSourceWindow struct {
	after   int64
	through int64
}

func (w ProcessSourceWindow) After() int64   { return w.after }
func (w ProcessSourceWindow) Through() int64 { return w.through }

type SourceWindowPolicy struct{}

func (SourceWindowPolicy) InitialState() source.Cursor { return source.NewCursor(0) }
func (SourceWindowPolicy) Activate(
	signal SourceHighWatermark,
	state source.Cursor,
) Transition[source.Cursor, ProcessSourceWindow] {
	if signal.sequence <= state.Sequence() {
		return NewTransition[source.Cursor, ProcessSourceWindow](state)
	}
	through := min(signal.sequence, state.Sequence()+signal.limit)
	return NewTransition(
		source.NewCursor(through),
		ProcessSourceWindow{after: state.Sequence(), through: through},
	)
}
