package trigger

import "slices"

// Transition is the complete result of one pure Trigger activation.
type Transition[S, A any] struct {
	state   S
	actions []A
}

func NewTransition[S, A any](state S, actions ...A) Transition[S, A] {
	return Transition[S, A]{state: state, actions: slices.Clone(actions)}
}

func (t Transition[S, A]) State() S     { return t.state }
func (t Transition[S, A]) Actions() []A { return slices.Clone(t.actions) }

// Policy maps one signal and activation state to a pure transition.
type Policy[Signal, State, Action any] interface {
	InitialState() State
	Activate(Signal, State) Transition[State, Action]
}
