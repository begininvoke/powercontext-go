package sqlstore_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/experience"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
	"github.com/ob-labs/powercontext-go/review"
	"github.com/ob-labs/powercontext-go/source"
)

func TestReviewApproveCommitsCandidateArtifactAndLineage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	sources, artifacts := repositories(t)
	candidates, err := sqlstore.NewCandidateRepository(
		sqlstore.SQLiteDialect,
		sqlstore.ExperienceArtifactCodec(),
		sqlstore.SkillArtifactCodec(),
	)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlstore.NewReviewBackend(
		database, "scope-review", candidates, artifacts, sources, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	service, err := review.NewService(backend, (&sequenceIDs{}).New)
	if err != nil {
		t.Fatal(err)
	}
	evidence := contentSource(t, "review-evidence", "ground truth", nil)
	var sourceRef sqlstore.StoredSource
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var addErr error
		sourceRef, addErr = sources.Add(ctx, tx, "scope-review", evidence)
		return addErr
	}); err != nil {
		t.Fatal(err)
	}
	proposal, err := experience.NewContent("situation", "action", "outcome", "lesson")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := service.ProposeExperience(
		ctx, proposal, []source.Ref{sourceRef.Ref, sourceRef.Ref}, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.Sources()) != 1 || candidate.Status() != review.Pending {
		t.Fatalf("candidate = %#v", candidate)
	}
	var proposalPayload []byte
	if err := database.SQLDB().QueryRowContext(ctx, `SELECT proposal FROM pc_artifact_candidate_versions
        WHERE scope_id = ? AND candidate_id = ? AND version = 1`,
		"scope-review", candidate.ID()).Scan(&proposalPayload); err != nil {
		t.Fatal(err)
	}
	wantProposal := `{"situation":"situation","action":"action","outcome":"outcome","lesson":"lesson"}`
	if string(proposalPayload) != wantProposal {
		t.Fatalf("proposal payload = %s", proposalPayload)
	}

	approved, err := service.Approve(ctx, candidate.ID(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status() != review.Approved || approved.ResultArtifact() == nil {
		t.Fatalf("approved = %#v", approved)
	}
	stored, err := artifacts.Get(ctx, database.SQLDB(), "scope-review", *approved.ResultArtifact())
	if err != nil {
		t.Fatal(err)
	}
	lineage := stored.(artifact.Artifact[experience.Content]).Lineage()
	if refs := lineage.Sources(); len(refs) != 1 || refs[0] != sourceRef.Ref {
		t.Fatalf("approved lineage = %#v", refs)
	}
	_, err = service.Approve(ctx, candidate.ID(), 1)
	var terminal *review.CandidateTerminalError
	if !errors.As(err, &terminal) {
		t.Fatalf("expected terminal error, got %v", err)
	}
}

func TestReviewApprovalRollsBackWhenProjectionFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	sources, artifacts := repositories(t)
	candidates, err := sqlstore.NewCandidateRepository(sqlstore.SQLiteDialect, sqlstore.ExperienceArtifactCodec())
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlstore.NewReviewBackend(
		database,
		"scope-review",
		candidates,
		artifacts,
		sources,
		failingExperienceIndex{failure: errors.New("index replacement failed")},
	)
	if err != nil {
		t.Fatal(err)
	}
	service, err := review.NewService(backend, (&sequenceIDs{}).New)
	if err != nil {
		t.Fatal(err)
	}
	evidence := contentSource(t, "review-evidence", "ground truth", nil)
	var sourceRef sqlstore.StoredSource
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var addErr error
		sourceRef, addErr = sources.Add(ctx, tx, "scope-review", evidence)
		return addErr
	}); err != nil {
		t.Fatal(err)
	}
	proposal, _ := experience.NewContent("s", "a", "o", "l")
	candidate, err := service.ProposeExperience(ctx, proposal, []source.Ref{sourceRef.Ref}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, candidate.ID(), 1); err == nil || err.Error() != "index replacement failed" {
		t.Fatalf("approval error = %v", err)
	}
	current, err := service.Get(ctx, candidate.ID())
	if err != nil {
		t.Fatal(err)
	}
	if current.Status() != review.Pending {
		t.Fatalf("candidate status after rollback = %s", current.Status())
	}
	var count int
	if err := database.SQLDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM pc_artifacts
        WHERE scope_id = ? AND family = ?`, "scope-review", experience.Family).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back Artifact count = %d", count)
	}
}

func TestSourceCursorRepositoryCASAndPythonPayload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	repository := sqlstore.SourceCursorRepository{}
	var first sqlstore.StoredSourceCursor
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var saveErr error
		first, saveErr = repository.Save(ctx, tx, "scope", "binding", source.NewCursor(7), nil)
		return saveErr
	}); err != nil {
		t.Fatal(err)
	}
	if first.Generation != 1 || first.Cursor.Sequence() != 7 {
		t.Fatalf("first cursor = %#v", first)
	}
	var payload []byte
	if err := database.SQLDB().QueryRowContext(ctx,
		`SELECT cursor FROM pc_source_cursors WHERE scope_id = ? AND binding_name = ?`,
		"scope", "binding").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"sequence":7}` {
		t.Fatalf("cursor payload = %s", payload)
	}
	expected := int64(1)
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		_, saveErr := repository.Save(ctx, tx, "scope", "binding", source.NewCursor(9), &expected)
		return saveErr
	}); err != nil {
		t.Fatal(err)
	}
	err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		_, saveErr := repository.Save(ctx, tx, "scope", "binding", source.NewCursor(10), &expected)
		return saveErr
	})
	var conflict *sqlstore.GenerationConflictError
	if !errors.As(err, &conflict) || conflict.Actual == nil || *conflict.Actual != 2 {
		t.Fatalf("expected generation conflict, got %v", err)
	}
}

type sequenceIDs struct {
	mu     sync.Mutex
	values map[string]int
}

func (f *sequenceIDs) New(kind string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.values == nil {
		f.values = make(map[string]int)
	}
	f.values[kind]++
	return fmt.Sprintf("%s-%d", kind, f.values[kind]), nil
}

type failingExperienceIndex struct{ failure error }

func (f failingExperienceIndex) Initialize(context.Context, sqlstore.DBTX) error { return nil }
func (f failingExperienceIndex) Replace(
	context.Context,
	sqlstore.DBTX,
	string,
	experience.Experience,
) error {
	return f.failure
}
func (f failingExperienceIndex) Search(
	context.Context,
	sqlstore.DBTX,
	string,
	string,
	int,
) ([]experience.SearchHit, error) {
	return nil, f.failure
}
