// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
