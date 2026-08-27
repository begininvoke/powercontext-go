package work

func encodeCurrentHandoff(value CurrentHandoff) (currentHandoffJSON, error) {
	if err := value.Validate(); err != nil {
		return currentHandoffJSON{}, err
	}
	state, err := encodeClaims(value.state)
	if err != nil {
		return currentHandoffJSON{}, err
	}
	var next *claimJSON
	if value.nextAction != nil {
		encoded, encodeErr := encodeClaim(*value.nextAction)
		if encodeErr != nil {
			return currentHandoffJSON{}, encodeErr
		}
		next = &encoded
	}
	return currentHandoffJSON{
		Schema: CurrentWorkHandoffSchema, Trust: UntrustedInput, Objective: value.objective,
		State: state, Disposition: value.disposition, NextAction: next, Omissions: nonNil(value.omissions),
	}, nil
}

func decodeCurrentHandoff(value currentHandoffJSON) (CurrentHandoff, error) {
	if value.Schema != CurrentWorkHandoffSchema || value.Trust != UntrustedInput {
		return CurrentHandoff{}, &InvalidError{Field: "handoff.schema", Detail: "does not match the current Work Handoff"}
	}
	state, err := decodeClaims(value.State)
	if err != nil {
		return CurrentHandoff{}, err
	}
	var next *Claim
	if value.NextAction != nil {
		decoded, decodeErr := decodeClaim(*value.NextAction)
		if decodeErr != nil {
			return CurrentHandoff{}, decodeErr
		}
		next = &decoded
	}
	return NewCurrentHandoff(value.Objective, state, value.Disposition, next, value.Omissions)
}
