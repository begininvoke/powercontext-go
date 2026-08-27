package work

import "github.com/ob-labs/powercontext-go/artifact"

func encodeHandoffReceipt(value HandoffReceipt) (handoffReceiptJSON, error) {
	if err := value.Validate(); err != nil {
		return handoffReceiptJSON{}, err
	}
	var selected *artifactRefJSON
	if value.selectedRevision != nil {
		encoded := encodeArtifactRef(*value.selectedRevision)
		selected = &encoded
	}
	var checks *receiverChecksJSON
	if value.receiverChecks != nil {
		checks = &receiverChecksJSON{
			LiveState: value.receiverChecks.liveState, Capability: value.receiverChecks.capability,
			Authorization: value.receiverChecks.authorization,
		}
	}
	evidence, err := encodeCitations(value.unavailableEvidence)
	if err != nil {
		return handoffReceiptJSON{}, err
	}
	return handoffReceiptJSON{
		Schema: HandoffReceiptSchema, Trust: UntrustedObservation, Receiver: value.receiver,
		Status: value.status, Selection: value.selection, SelectedRevision: selected,
		PreparedDigest: cloneString(value.preparedDigest), ReceiverChecks: checks,
		EvidenceStatus: value.evidenceStatus, UnavailableEvidence: evidence, Message: cloneString(value.message),
	}, nil
}

func decodeHandoffReceipt(value handoffReceiptJSON) (HandoffReceipt, error) {
	if value.Schema != HandoffReceiptSchema || value.Trust != UntrustedObservation {
		return HandoffReceipt{}, &InvalidError{Field: "receipt.schema", Detail: "does not match the Handoff receipt"}
	}
	var selected *artifact.Ref
	if value.SelectedRevision != nil {
		decoded, err := decodeArtifactRef(*value.SelectedRevision)
		if err != nil {
			return HandoffReceipt{}, err
		}
		selected = &decoded
	}
	var checks *ReceiverChecks
	if value.ReceiverChecks != nil {
		decoded, err := NewReceiverChecks(value.ReceiverChecks.LiveState, value.ReceiverChecks.Capability, value.ReceiverChecks.Authorization)
		if err != nil {
			return HandoffReceipt{}, err
		}
		checks = &decoded
	}
	evidence, err := decodeCitations(value.UnavailableEvidence)
	if err != nil {
		return HandoffReceipt{}, err
	}
	return NewHandoffReceipt(
		value.Receiver, value.Status, value.Selection, selected, value.PreparedDigest,
		checks, value.EvidenceStatus, evidence, value.Message,
	)
}
