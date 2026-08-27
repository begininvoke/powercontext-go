package endpoint

import (
	"context"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/internal/runtime"
	"github.com/ob-labs/powercontext-go/internal/work"
	"github.com/ob-labs/powercontext-go/source"
)

type WorkOperations interface {
	CreateContract(context.Context, string, work.CreateContract) (work.SourceReceipt, error)
	HandoffCurrent(context.Context, string, work.HandoffCurrent) (work.PreparedHandoff, error)
	Acknowledge(context.Context, string, work.Acknowledge) (work.Acknowledgement, error)
	RecordOutcome(context.Context, string, work.RecordOutcome) (work.SourceReceipt, error)
}

func (h *Handler) CreateWorkContract(ctx context.Context, req *v1.CreateWorkContractRequest) (v1.CreateWorkContractRes, error) {
	if h.work == nil {
		return nil, &RuntimeNotReadyError{}
	}
	contract, err := runtimeWorkContract(req.Contract)
	if err != nil {
		return nil, err
	}
	receipt, err := h.work.CreateContract(ctx, req.ScopeID, work.CreateContract{SourceID: req.SourceID, Contract: contract})
	if err != nil {
		return nil, err
	}
	return workSourceReceiptResponse(ctx, receipt), nil
}

func (h *Handler) HandoffCurrentWork(ctx context.Context, req *v1.HandoffCurrentWorkRequest) (v1.HandoffCurrentWorkRes, error) {
	if h.work == nil {
		return nil, &RuntimeNotReadyError{}
	}
	current, err := runtimeCurrentWorkHandoff(req.Handoff)
	if err != nil {
		return nil, err
	}
	prepared, err := h.work.HandoffCurrent(ctx, req.ScopeID, work.HandoffCurrent{SourceID: req.SourceID, Handoff: current})
	if err != nil {
		return nil, err
	}
	wireHandoff, err := wirePreparedHandoff(prepared.Handoff)
	if err != nil {
		return nil, err
	}
	return &v1.PreparedWorkHandoffHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response:               v1.PreparedWorkHandoff{Boundary: wireWorkSourceReceipt(prepared.Boundary), Handoff: wireHandoff},
	}, nil
}

func (h *Handler) AcknowledgeHandoff(ctx context.Context, req *v1.AcknowledgeHandoffRequest) (v1.AcknowledgeHandoffRes, error) {
	if h.work == nil {
		return nil, &RuntimeNotReadyError{}
	}
	request, err := runtimeAcknowledgeHandoff(*req)
	if err != nil {
		return nil, err
	}
	acknowledgement, err := h.work.Acknowledge(ctx, req.ScopeID, request)
	if err != nil {
		return nil, err
	}
	resolution, err := wireHandoffResolution(acknowledgement.Resolution)
	if err != nil {
		return nil, err
	}
	return &v1.HandoffAcknowledgementHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response: v1.HandoffAcknowledgement{
			Resolution: resolution, Receipt: wireWorkSourceReceipt(acknowledgement.Receipt),
		},
	}, nil
}

func (h *Handler) RecordTaskOutcome(ctx context.Context, req *v1.RecordTaskOutcomeRequest) (v1.RecordTaskOutcomeRes, error) {
	if h.work == nil {
		return nil, &RuntimeNotReadyError{}
	}
	outcome, err := runtimeTaskOutcome(req.Outcome)
	if err != nil {
		return nil, err
	}
	receipt, err := h.work.RecordOutcome(ctx, req.ScopeID, work.RecordOutcome{SourceID: req.SourceID, Outcome: outcome})
	if err != nil {
		return nil, err
	}
	return workSourceReceiptResponse(ctx, receipt), nil
}

func runtimeWorkContract(value v1.WorkContract) (work.Contract, error) {
	if value.Schema != v1.WorkContractSchemaPowercontextWorkContractV1 || value.Trust != v1.WorkContractTrustUntrustedInput {
		return work.Contract{}, invalidWorkRequest("contract.schema")
	}
	facts, err := runtimeWorkClaims(value.Facts)
	if err != nil {
		return work.Contract{}, err
	}
	result, err := work.NewContract(
		value.Objective, facts, value.InScope, value.Exclusions, value.CompletionCriteria,
		value.AuthorizationNotes, value.OpenQuestions,
	)
	if err != nil {
		return work.Contract{}, invalidWorkRequest("contract")
	}
	return result, nil
}

func runtimeCurrentWorkHandoff(value v1.CurrentWorkHandoff) (work.CurrentHandoff, error) {
	if value.Schema != v1.CurrentWorkHandoffSchemaPowercontextCurrentWorkHandoffV1 || value.Trust != v1.CurrentWorkHandoffTrustUntrustedInput {
		return work.CurrentHandoff{}, invalidWorkRequest("handoff.schema")
	}
	state, err := runtimeWorkClaims(value.State)
	if err != nil {
		return work.CurrentHandoff{}, err
	}
	var next *work.Claim
	if nextValue, ok := value.NextAction.Get(); ok {
		mapped, mapErr := runtimeWorkClaim(nextValue)
		if mapErr != nil {
			return work.CurrentHandoff{}, mapErr
		}
		next = &mapped
	}
	result, err := work.NewCurrentHandoff(
		value.Objective, state, handoff.Disposition(value.Disposition), next, value.Omissions,
	)
	if err != nil {
		return work.CurrentHandoff{}, invalidWorkRequest("handoff")
	}
	return result, nil
}

func runtimeTaskOutcome(value v1.TaskOutcome) (work.TaskOutcome, error) {
	if value.Schema != v1.TaskOutcomeSchemaPowercontextTaskOutcomeV1 || value.Trust != v1.TaskOutcomeTrustUntrustedObservation {
		return work.TaskOutcome{}, invalidWorkRequest("outcome.schema")
	}
	observations, err := runtimeWorkClaims(value.Observations)
	if err != nil {
		return work.TaskOutcome{}, err
	}
	checks := make([]work.TaskCheck, len(value.Checks))
	for index, check := range value.Checks {
		evidence, mapErr := runtimeHandoffCitations(check.Evidence)
		if mapErr != nil {
			return work.TaskOutcome{}, mapErr
		}
		checks[index], mapErr = work.NewTaskCheck(
			check.Name, work.CheckStatus(check.Status), optionalString(check.Details),
			work.ClaimBasis(check.Basis), evidence,
		)
		if mapErr != nil {
			return work.TaskOutcome{}, invalidWorkRequest("outcome.checks")
		}
	}
	artifacts, err := runtimeArtifactReferences(value.ProducedArtifacts)
	if err != nil {
		return work.TaskOutcome{}, err
	}
	var receipt *source.Ref
	if sourceValue, ok := value.HandoffReceiptRef.Get(); ok {
		mapped, mapErr := source.NewRef(sourceValue.Name, sourceValue.SourceID)
		if mapErr != nil {
			return work.TaskOutcome{}, invalidWorkRequest("outcome.handoff_receipt_ref")
		}
		receipt = &mapped
	}
	result, err := work.NewTaskOutcome(
		value.Objective, work.OutcomeStatus(value.Status), value.Summary, receipt,
		observations, checks, artifacts, value.RemainingWork,
	)
	if err != nil {
		return work.TaskOutcome{}, invalidWorkRequest("outcome")
	}
	return result, nil
}

func runtimeAcknowledgeHandoff(value v1.AcknowledgeHandoffRequest) (work.Acknowledge, error) {
	var checks *work.ReceiverChecks
	if checkValue, ok := value.ReceiverChecks.Get(); ok {
		mapped, err := work.NewReceiverChecks(
			work.LiveStateCheckStatus(checkValue.LiveState), work.ReadinessCheckStatus(checkValue.Capability),
			work.ReadinessCheckStatus(checkValue.Authorization),
		)
		if err != nil {
			return work.Acknowledge{}, invalidWorkRequest("receiver_checks")
		}
		checks = &mapped
	}
	var prepared *handoff.Prepared
	if preparedValue, ok := value.Prepared.Get(); ok {
		mapped, err := runtimePreparedHandoff(preparedValue)
		if err != nil {
			return work.Acknowledge{}, err
		}
		prepared = &mapped
	}
	var revision *artifact.Ref
	if revisionValue, ok := value.Revision.Get(); ok {
		mapped, err := runtimeArtifactReference(revisionValue)
		if err != nil {
			return work.Acknowledge{}, invalidWorkRequest("revision")
		}
		revision = &mapped
	}
	result, err := work.NewAcknowledge(
		value.SourceID, value.Receiver, work.ReceiptStatus(value.Status), handoff.Selection(value.Selection),
		checks, prepared, revision, optionalString(value.Message),
	)
	if err != nil {
		return work.Acknowledge{}, invalidWorkRequest("acknowledgement")
	}
	return result, nil
}

func runtimeWorkClaims(values []v1.WorkClaim) ([]work.Claim, error) {
	result := make([]work.Claim, len(values))
	for index, value := range values {
		mapped, err := runtimeWorkClaim(value)
		if err != nil {
			return nil, err
		}
		result[index] = mapped
	}
	return result, nil
}

func runtimeWorkClaim(value v1.WorkClaim) (work.Claim, error) {
	evidence, err := runtimeHandoffCitations(value.Evidence)
	if err != nil {
		return work.Claim{}, err
	}
	result, err := work.NewClaim(value.Text, work.ClaimBasis(value.Basis), evidence)
	if err != nil {
		return work.Claim{}, invalidWorkRequest("claim")
	}
	return result, nil
}

func runtimeArtifactReferences(values []v1.ArtifactReference) ([]artifact.Ref, error) {
	result := make([]artifact.Ref, len(values))
	for index, value := range values {
		ref, err := runtimeArtifactReference(value)
		if err != nil {
			return nil, invalidWorkRequest("artifact_ref")
		}
		result[index] = ref
	}
	return result, nil
}

func workSourceReceiptResponse(ctx context.Context, value work.SourceReceipt) *v1.WorkSourceReceiptHeaders {
	return &v1.WorkSourceReceiptHeaders{XPowerContextRequestID: requestID(ctx), Response: wireWorkSourceReceipt(value)}
}

func wireWorkSourceReceipt(value work.SourceReceipt) v1.WorkSourceReceipt {
	return v1.WorkSourceReceipt{
		Kind: v1.WorkSourceKind(value.Kind), Source: sourceReference(value.SourceRef),
		Position: int(value.Position), ContentDigest: value.ContentDigest,
	}
}

func invalidWorkRequest(field string) error { return &InvalidRequestError{Field: field} }

var _ WorkOperations = (*runtime.WorkApplication)(nil)
