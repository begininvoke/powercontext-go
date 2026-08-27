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

package handoff

import (
	"fmt"
	"reflect"
	"slices"
)

type Statement struct {
	text      string
	citations []Citation
}

func NewStatement(text string, citations []Citation) (Statement, error) {
	value := Statement{text: text, citations: slices.Clone(citations)}
	if err := value.Validate(); err != nil {
		return Statement{}, err
	}
	return value, nil
}
func (s Statement) Text() string          { return s.text }
func (s Statement) Citations() []Citation { return slices.Clone(s.citations) }
func (s Statement) Validate() error {
	if err := validateText("statement", s.text); err != nil {
		return err
	}
	if len(s.citations) < 1 || len(s.citations) > MaxCitations {
		return fmt.Errorf("Handoff statement must contain 1..%d citations", MaxCitations)
	}
	return validateCitations(s.citations, false)
}

type Omission struct {
	text     string
	citation Citation
}

func NewOmission(text string, citation Citation) (Omission, error) {
	value := Omission{text: text, citation: citation}
	if err := value.Validate(); err != nil {
		return Omission{}, err
	}
	return value, nil
}
func (o Omission) Text() string       { return o.text }
func (o Omission) Citation() Citation { return o.citation }
func (o Omission) Validate() error {
	if err := validateText("omission", o.text); err != nil {
		return err
	}
	if o.citation != nil {
		return validateCitation(o.citation)
	}
	return nil
}

type Content struct {
	objective   string
	state       []Statement
	disposition Disposition
	nextAction  *Statement
	omissions   []Omission
}

func NewContent(objective string, state []Statement, disposition Disposition, nextAction *Statement, omissions []Omission) (Content, error) {
	value := Content{
		objective: objective, state: slices.Clone(state), disposition: disposition,
		nextAction: cloneStatement(nextAction), omissions: slices.Clone(omissions),
	}
	if err := value.Validate(); err != nil {
		return Content{}, err
	}
	return value, nil
}

func (c Content) Schema() string           { return ContentSchemaVersion }
func (c Content) Objective() string        { return c.objective }
func (c Content) State() []Statement       { return slices.Clone(c.state) }
func (c Content) Disposition() Disposition { return c.disposition }
func (c Content) NextAction() *Statement   { return cloneStatement(c.nextAction) }
func (c Content) Omissions() []Omission    { return slices.Clone(c.omissions) }
func (c Content) Equal(other Content) bool { return reflect.DeepEqual(c, other) }
func (c Content) Validate() error {
	if err := validateText("objective", c.objective); err != nil {
		return err
	}
	if len(c.state) < 1 || len(c.state) > MaxStateStatements {
		return fmt.Errorf("Handoff state must contain 1..%d statements", MaxStateStatements)
	}
	for _, statement := range c.state {
		if err := statement.Validate(); err != nil {
			return err
		}
	}
	if c.disposition != Continuable && c.disposition != Blocked && c.disposition != Complete {
		return fmt.Errorf("invalid Handoff disposition %q", c.disposition)
	}
	if c.nextAction != nil {
		if err := c.nextAction.Validate(); err != nil {
			return err
		}
	}
	if len(c.omissions) > MaxOmissions {
		return fmt.Errorf("Handoff omissions exceed %d items", MaxOmissions)
	}
	for _, omission := range c.omissions {
		if err := omission.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type Draft struct{ content Content }

func NewDraft(objective string, state []Statement, disposition Disposition, nextAction *Statement, omissions []Omission) (Draft, error) {
	content, err := NewContent(objective, state, disposition, nextAction, omissions)
	if err != nil {
		return Draft{}, err
	}
	return Draft{content: content}, nil
}
func (d Draft) AsContent() Content { return cloneContent(d.content) }
func (d Draft) Objective() string  { return d.content.objective }
func (d Draft) Validate() error    { return d.content.Validate() }
