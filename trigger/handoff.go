package trigger

import (
	"fmt"

	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/source"
)

const HandoffBoundaryName = "handoff-participant-boundary"

type HandoffBoundary struct {
	position   int64
	activation handoff.Activate
}

func NewHandoffBoundary(position int64, activation handoff.Activate) (HandoffBoundary, error) {
	if position < 1 {
		return HandoffBoundary{}, fmt.Errorf("Handoff boundary position must be positive")
	}
	if err := activation.Validate(); err != nil {
		return HandoffBoundary{}, err
	}
	return HandoffBoundary{position: position, activation: activation.Clone()}, nil
}

func (b HandoffBoundary) Position() int64              { return b.position }
func (b HandoffBoundary) Activation() handoff.Activate { return b.activation.Clone() }

type HandoffBoundaryPolicy struct{}

func (HandoffBoundaryPolicy) InitialState() source.Cursor { return source.NewCursor(0) }
func (HandoffBoundaryPolicy) Activate(
	signal HandoffBoundary,
	state source.Cursor,
) Transition[source.Cursor, handoff.Prepare] {
	if signal.position <= state.Sequence() {
		return NewTransition[source.Cursor, handoff.Prepare](state)
	}
	activation := signal.activation.Clone()
	action, err := handoff.NewPrepare(
		activation.Objective(),
		activation.ActionEvidence(),
		activation.MaxBytes(),
	)
	if err != nil {
		// HandoffBoundary can only contain a validated Activate value.
		panic(err)
	}
	return NewTransition(source.NewCursor(signal.position), action)
}
