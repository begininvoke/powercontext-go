package work

func encodeContract(value Contract) (contractJSON, error) {
	if err := value.Validate(); err != nil {
		return contractJSON{}, err
	}
	facts, err := encodeClaims(value.facts)
	if err != nil {
		return contractJSON{}, err
	}
	return contractJSON{
		Schema: WorkContractSchema, Trust: UntrustedInput, Objective: value.objective,
		Facts: facts, InScope: nonNil(value.inScope), Exclusions: nonNil(value.exclusions),
		CompletionCriteria: nonNil(value.completionCriteria), AuthorizationNotes: nonNil(value.authorizationNotes),
		OpenQuestions: nonNil(value.openQuestions),
	}, nil
}

func decodeContract(value contractJSON) (Contract, error) {
	if value.Schema != WorkContractSchema || value.Trust != UntrustedInput {
		return Contract{}, &InvalidError{Field: "contract.schema", Detail: "does not match the Work contract"}
	}
	facts, err := decodeClaims(value.Facts)
	if err != nil {
		return Contract{}, err
	}
	return NewContract(value.Objective, facts, value.InScope, value.Exclusions, value.CompletionCriteria, value.AuthorizationNotes, value.OpenQuestions)
}
