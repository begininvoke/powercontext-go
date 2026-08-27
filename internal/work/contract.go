package work

import "slices"

type Contract struct {
	objective          string
	facts              []Claim
	inScope            []string
	exclusions         []string
	completionCriteria []string
	authorizationNotes []string
	openQuestions      []string
}

func NewContract(
	objective string,
	facts []Claim,
	inScope, exclusions, completionCriteria, authorizationNotes, openQuestions []string,
) (Contract, error) {
	value := Contract{
		objective: objective, facts: slices.Clone(facts), inScope: slices.Clone(inScope),
		exclusions: slices.Clone(exclusions), completionCriteria: slices.Clone(completionCriteria),
		authorizationNotes: slices.Clone(authorizationNotes), openQuestions: slices.Clone(openQuestions),
	}
	if err := value.Validate(); err != nil {
		return Contract{}, err
	}
	return value, nil
}

func (c Contract) Objective() string            { return c.objective }
func (c Contract) Facts() []Claim               { return slices.Clone(c.facts) }
func (c Contract) InScope() []string            { return slices.Clone(c.inScope) }
func (c Contract) Exclusions() []string         { return slices.Clone(c.exclusions) }
func (c Contract) CompletionCriteria() []string { return slices.Clone(c.completionCriteria) }
func (c Contract) AuthorizationNotes() []string { return slices.Clone(c.authorizationNotes) }
func (c Contract) OpenQuestions() []string      { return slices.Clone(c.openQuestions) }
func (c Contract) Schema() string               { return WorkContractSchema }
func (c Contract) Trust() string                { return UntrustedInput }
func (c Contract) Validate() error {
	if err := validateText("contract.objective", c.objective, MaxTextLength); err != nil {
		return err
	}
	if err := validateClaims("contract.facts", c.facts, 0, MaxItems); err != nil {
		return err
	}
	for _, values := range []struct {
		name    string
		values  []string
		minimum int
	}{
		{"contract.in_scope", c.inScope, 1},
		{"contract.exclusions", c.exclusions, 0},
		{"contract.completion_criteria", c.completionCriteria, 1},
		{"contract.authorization_notes", c.authorizationNotes, 0},
		{"contract.open_questions", c.openQuestions, 0},
	} {
		if err := validateTextItems(values.name, values.values, values.minimum, MaxItems); err != nil {
			return err
		}
	}
	return nil
}
