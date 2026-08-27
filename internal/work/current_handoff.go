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

package work

import (
	"slices"

	"github.com/ob-labs/powercontext-go/artifact/handoff"
)

type CurrentHandoff struct {
	objective   string
	state       []Claim
	disposition handoff.Disposition
	nextAction  *Claim
	omissions   []string
}

func NewCurrentHandoff(
	objective string,
	state []Claim,
	disposition handoff.Disposition,
	nextAction *Claim,
	omissions []string,
) (CurrentHandoff, error) {
	value := CurrentHandoff{
		objective: objective, state: slices.Clone(state), disposition: disposition,
		nextAction: cloneClaim(nextAction), omissions: slices.Clone(omissions),
	}
	if err := value.Validate(); err != nil {
		return CurrentHandoff{}, err
	}
	return value, nil
}

func (h CurrentHandoff) Schema() string                   { return CurrentWorkHandoffSchema }
func (h CurrentHandoff) Trust() string                    { return UntrustedInput }
func (h CurrentHandoff) Objective() string                { return h.objective }
func (h CurrentHandoff) State() []Claim                   { return slices.Clone(h.state) }
func (h CurrentHandoff) Disposition() handoff.Disposition { return h.disposition }
func (h CurrentHandoff) NextAction() *Claim               { return cloneClaim(h.nextAction) }
func (h CurrentHandoff) Omissions() []string              { return slices.Clone(h.omissions) }
func (h CurrentHandoff) Validate() error {
	if err := validateText("handoff.objective", h.objective, MaxTextLength); err != nil {
		return err
	}
	if err := validateClaims("handoff.state", h.state, 1, MaxItems); err != nil {
		return err
	}
	if h.disposition != handoff.Continuable && h.disposition != handoff.Blocked && h.disposition != handoff.Complete {
		return &InvalidError{Field: "handoff.disposition", Detail: "has an unsupported value"}
	}
	if h.nextAction != nil {
		if err := h.nextAction.Validate(); err != nil {
			return err
		}
	}
	return validateTextItems("handoff.omissions", h.omissions, 0, MaxItems)
}
