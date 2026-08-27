package handoff

import (
	"fmt"

	"github.com/ob-labs/powercontext-go/source"
)

type ActivationStatus string

const (
	ActivationGenerated ActivationStatus = "generated"
	ActivationIgnored   ActivationStatus = "ignored"
)

type Activation struct {
	status           ActivationStatus
	boundarySource   source.Ref
	previousPosition int64
	currentPosition  int64
	draft            *Draft
}

func NewActivation(
	status ActivationStatus,
	boundarySource source.Ref,
	previousPosition int64,
	currentPosition int64,
	draft *Draft,
) (Activation, error) {
	value := Activation{
		status: status, boundarySource: boundarySource,
		previousPosition: previousPosition, currentPosition: currentPosition,
		draft: cloneDraft(draft),
	}
	if err := value.Validate(); err != nil {
		return Activation{}, err
	}
	return value, nil
}

func (a Activation) Status() ActivationStatus   { return a.status }
func (a Activation) BoundarySource() source.Ref { return a.boundarySource }
func (a Activation) PreviousPosition() int64    { return a.previousPosition }
func (a Activation) CurrentPosition() int64     { return a.currentPosition }
func (a Activation) Draft() *Draft              { return cloneDraft(a.draft) }
func (a Activation) Validate() error {
	if _, err := source.NewRef(a.boundarySource.Type(), a.boundarySource.ID()); err != nil {
		return err
	}
	if a.previousPosition < 0 || a.currentPosition < 0 {
		return fmt.Errorf("Handoff activation positions must not be negative")
	}
	if a.currentPosition < a.previousPosition {
		return fmt.Errorf("Handoff activation position cannot move backwards")
	}
	switch a.status {
	case ActivationGenerated:
		if a.draft == nil || a.currentPosition <= a.previousPosition {
			return fmt.Errorf("generated Handoff activation must advance with a Draft")
		}
		return a.draft.Validate()
	case ActivationIgnored:
		if a.draft != nil || a.currentPosition != a.previousPosition {
			return fmt.Errorf("ignored Handoff activation cannot change state or contain a Draft")
		}
		return nil
	default:
		return fmt.Errorf("invalid Handoff activation status %q", a.status)
	}
}
