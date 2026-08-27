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

package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	pcRuntime "github.com/ob-labs/powercontext-go/internal/runtime"
	"github.com/ob-labs/powercontext-go/internal/work"
	"github.com/ob-labs/powercontext-go/source"
)

func TestAcknowledgementCannotAcceptUnavailableHandoffEvidence(t *testing.T) {
	t.Parallel()
	application, sources, resolver := newWorkApplication(t)
	missing := workApplicationCitation(t, "missing-output")
	resolver.unavailable = map[string]struct{}{missing.Ref().ID(): {}}
	statement, err := handoff.NewStatement("The change was reported as implemented.", []handoff.Citation{missing})
	if err != nil {
		t.Fatal(err)
	}
	content, err := handoff.NewContent(
		"Continue a partially verified change.", []handoff.Statement{statement}, handoff.Continuable, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := handoff.NewPrepared("project", nil, content)
	if err != nil {
		t.Fatal(err)
	}
	checks := confirmedReceiverChecks(t)
	accepted, err := work.NewAcknowledge(
		"receipt-accepted", "receiver-agent", work.ReceiptAccepted, handoff.PreparedSelection,
		&checks, &prepared, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.Acknowledge(t.Context(), "project", accepted)
	var invalid *work.InvalidRequestError
	if !errors.As(err, &invalid) || invalid.Code != "handoff-evidence-unavailable" {
		t.Fatalf("accepted error = %#v", err)
	}

	message := "The cited implementation output is unavailable."
	clarification, err := work.NewAcknowledge(
		"receipt-clarification", "receiver-agent", work.ReceiptNeedsClarification, handoff.PreparedSelection,
		nil, &prepared, nil, &message,
	)
	if err != nil {
		t.Fatal(err)
	}
	acknowledged, err := application.Acknowledge(t.Context(), "project", clarification)
	if err != nil {
		t.Fatal(err)
	}
	evidenceChecks := acknowledged.Resolution.EvidenceChecks()
	if len(evidenceChecks) != 1 || evidenceChecks[0].Status() != handoff.EvidenceUnavailable ||
		acknowledged.Receipt.Kind != work.HandoffReceiptSourceKind || acknowledged.Receipt.Position != 1 {
		t.Fatalf("clarification = %#v", acknowledged)
	}
	if len(sources.entries) != 1 {
		t.Fatalf("captured entries = %d, want 1", len(sources.entries))
	}
}

func TestWorkContractRejectsVerifiedClaimWithMissingEvidence(t *testing.T) {
	t.Parallel()
	application, sources, resolver := newWorkApplication(t)
	missing := workApplicationCitation(t, "missing-test-output")
	resolver.unavailable = map[string]struct{}{missing.Ref().ID(): {}}
	claim, err := work.NewClaim("A regression test passed.", work.Verified, []handoff.Citation{missing})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := work.NewContract(
		"Use only evidence-backed facts.", []work.Claim{claim},
		[]string{"Record a grounded delegation baseline."}, nil,
		[]string{"Reject unavailable verified evidence."}, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.CreateContract(t.Context(), "project", work.CreateContract{SourceID: "contract-1", Contract: contract})
	var unavailable *handoff.EvidenceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %#v, want EvidenceUnavailableError", err)
	}
	if len(sources.entries) != 0 {
		t.Fatal("failed Contract must not be captured")
	}
}

func TestTaskOutcomeRejectsNonReceiptResultLink(t *testing.T) {
	t.Parallel()
	application, sources, _ := newWorkApplication(t)
	missing, err := source.NewRef(source.ContentType, "missing-receipt")
	if err != nil {
		t.Fatal(err)
	}
	observation, err := work.NewClaim("No accepted Receipt was found.", work.Declared, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := work.NewTaskOutcome(
		"Link one exact accepted receipt.", work.OutcomeUnknown, "The requested Receipt does not exist.",
		&missing, []work.Claim{observation}, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.RecordOutcome(t.Context(), "project", work.RecordOutcome{SourceID: "outcome-1", Outcome: outcome})
	var invalid *work.InvalidRequestError
	if !errors.As(err, &invalid) || invalid.Code != "task-outcome-handoff-receipt" {
		t.Fatalf("error = %#v", err)
	}
	if len(sources.entries) != 0 {
		t.Fatal("invalid Outcome must not be captured")
	}
}

func newWorkApplication(t *testing.T) (*pcRuntime.WorkApplication, *workApplicationSources, *workApplicationResolver) {
	t.Helper()
	lifecycle := pcRuntime.New()
	t.Cleanup(func() {
		if err := lifecycle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	sources := new(workApplicationSources)
	resolver := &workApplicationResolver{unavailable: map[string]struct{}{}}
	backend := new(workApplicationHandoffBackend)
	factory := func(scopeID string) (*handoff.Service, error) {
		return handoff.NewService(scopeID, pcRuntime.DefaultHandoffArtifactID, backend, resolver, nil)
	}
	application, err := pcRuntime.NewWorkApplication(lifecycle, sources, factory)
	if err != nil {
		t.Fatal(err)
	}
	return application, sources, resolver
}

type workApplicationSources struct {
	entries []source.JournalEntry
}

func (s *workApplicationSources) Capture(
	ctx context.Context,
	_ string,
	capture source.ContentCapture,
) (source.Ref, int64, error) {
	value, err := (source.ContentAdapter{}).Resolve(ctx, capture)
	if err != nil {
		return source.Ref{}, 0, err
	}
	ref, err := source.NewRef(source.ContentType, capture.ID())
	if err != nil {
		return source.Ref{}, 0, err
	}
	position := int64(len(s.entries) + 1)
	entry, err := source.NewJournalEntry(ref, value, position)
	if err != nil {
		return source.Ref{}, 0, err
	}
	s.entries = append(s.entries, entry)
	return ref, position, nil
}

func (s *workApplicationSources) Entries(context.Context, string) ([]source.JournalEntry, error) {
	return slices.Clone(s.entries), nil
}

type workApplicationResolver struct {
	unavailable map[string]struct{}
}

func (r *workApplicationResolver) Resolve(ctx context.Context, citation handoff.Citation) (handoff.Evidence, error) {
	if err := r.Validate(ctx, citation); err != nil {
		return nil, err
	}
	value, ok := citation.(handoff.SourceCitation)
	if !ok {
		return nil, fmt.Errorf("unsupported citation %T", citation)
	}
	snapshot, err := source.RestoreContentSource(value.Ref().ID(), source.Captured, nil, "", nil)
	if err != nil {
		return nil, err
	}
	return handoff.NewSourceEvidence(value, snapshot)
}

func (r *workApplicationResolver) Validate(_ context.Context, citation handoff.Citation) error {
	value, ok := citation.(handoff.SourceCitation)
	if ok {
		if _, missing := r.unavailable[value.Ref().ID()]; missing {
			return &handoff.EvidenceUnavailableError{Citation: citation}
		}
	}
	return nil
}

type workApplicationHandoffBackend struct{}

func (*workApplicationHandoffBackend) Create(context.Context, string, handoff.ArtifactDraft) (handoff.Handoff, error) {
	return handoff.Handoff{}, errors.New("unexpected Create")
}

func (*workApplicationHandoffBackend) Revise(context.Context, handoff.Handoff, handoff.ArtifactDraft) (handoff.Handoff, error) {
	return handoff.Handoff{}, errors.New("unexpected Revise")
}

func (*workApplicationHandoffBackend) Get(context.Context, artifact.Ref) (handoff.Handoff, error) {
	return handoff.Handoff{}, errors.New("unexpected Get")
}

func (*workApplicationHandoffBackend) Latest(context.Context, string) (handoff.Handoff, bool, error) {
	return handoff.Handoff{}, false, nil
}

func (*workApplicationHandoffBackend) Revisions(context.Context, string) ([]handoff.Handoff, error) {
	return nil, nil
}

func workApplicationCitation(t *testing.T, sourceID string) handoff.SourceCitation {
	t.Helper()
	ref, err := source.NewRef(source.ContentType, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	citation, err := handoff.NewSourceCitation(ref)
	if err != nil {
		t.Fatal(err)
	}
	return citation
}

func confirmedReceiverChecks(t *testing.T) work.ReceiverChecks {
	t.Helper()
	checks, err := work.NewReceiverChecks(work.LiveStateConfirmed, work.ReadinessConfirmed, work.ReadinessConfirmed)
	if err != nil {
		t.Fatal(err)
	}
	return checks
}
