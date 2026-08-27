package sqlstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/artifact/skill"
	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
	"github.com/ob-labs/powercontext-go/internal/stats"
	"github.com/ob-labs/powercontext-go/source"
	"github.com/ob-labs/powercontext-go/trigger"
)

func TestExternalSkillReplacementIsScopedAtomicAndDeterministic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	store, err := sqlstore.NewExternalSkillStore(database, "project:example")
	if err != nil {
		t.Fatal(err)
	}
	first := externalRegistration(t, "friendly-python", "/workspace/friendly-python", "a")
	second := externalRegistration(t, "piglet", "/workspace/piglet", "b")
	if _, err := store.Replace(ctx, []string{"codex"}, "workstation-1", []skill.Registration{second, first}); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Name() != "friendly-python" || listed[1].Name() != "piglet" {
		t.Fatalf("unexpected registration order: %#v", listed)
	}

	// Two identities cannot claim one provider/host/scope/locator binding. The
	// replacement deletes first, so this also proves the surrounding transaction
	// restores the preceding projection on insert failure.
	conflict := externalRegistration(t, "conflict", first.Locator(), "c")
	if _, err := store.Replace(ctx, []string{"codex"}, "workstation-1", []skill.Registration{first, conflict}); err == nil {
		t.Fatal("expected binding conflict")
	}
	listed, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Fingerprint() != first.Fingerprint() {
		t.Fatalf("failed replacement changed the prior snapshot: %#v", listed)
	}

	other, err := sqlstore.NewExternalSkillStore(database, "project:other")
	if err != nil {
		t.Fatal(err)
	}
	_, err = other.Get(ctx, first.ExternalSkillID())
	var missing *skill.ExternalNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("expected scoped not found, got %v", err)
	}
}

func TestExternalSkillReplacementAtomicallyCoversAllAgentProviders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	store, err := sqlstore.NewExternalSkillStore(database, "project:example")
	if err != nil {
		t.Fatal(err)
	}
	codex := externalRegistration(t, "codex-review", "/workspace/codex-review", "a")
	claude := externalRegistrationForProvider(
		t, "claude_code", "claude-review", "/workspace/claude-review", "b",
	)
	providers := []string{"codex", "claude_code"}
	if _, err := store.Replace(ctx, providers, "workstation-1", []skill.Registration{codex, claude}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Replace(ctx, providers, "workstation-1", []skill.Registration{codex}); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Provider() != "codex" {
		t.Fatalf("mixed-provider replacement left stale registrations: %#v", listed)
	}
}

func TestHandoffBackendUsesArtifactCASAndDomainNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	sources, artifacts := repositories(t)
	var storedSource sqlstore.StoredSource
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var err error
		storedSource, err = sources.Add(ctx, tx, "scope-a", contentSource(t, "evidence", "ready", nil))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	backend, err := sqlstore.NewHandoffBackend(database, "scope-a", artifacts)
	if err != nil {
		t.Fatal(err)
	}
	citation, _ := handoff.NewSourceCitation(storedSource.Ref)
	statement, _ := handoff.NewStatement("Ready", []handoff.Citation{citation})
	content, _ := handoff.NewContent("Ship", []handoff.Statement{statement}, handoff.Continuable, nil, nil)
	draft, _ := handoff.NewArtifactDraft(content, []source.Ref{storedSource.Ref}, nil)
	created, err := backend.Create(ctx, "handoff-1", draft)
	if err != nil {
		t.Fatal(err)
	}
	latest, found, err := backend.Latest(ctx, "handoff-1")
	if err != nil || !found || latest.Ref() != created.Ref() {
		t.Fatalf("latest = %#v, %v, %v", latest, found, err)
	}
	if _, err := backend.Revise(ctx, created, draft); err != nil {
		t.Fatal(err)
	}
	revisions, err := backend.Revisions(ctx, "handoff-1")
	if err != nil || len(revisions) != 2 {
		t.Fatalf("revisions = %d, %v", len(revisions), err)
	}
	missingRef, _ := artifact.NewRef(handoff.Family, "missing", 1)
	_, err = backend.Get(ctx, missingRef)
	var missing *artifact.NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("expected Artifact NotFound, got %v", err)
	}
}

func TestScopedStatisticsPreservesIncompleteUsageAndRecallProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	_, artifacts := repositories(t)
	repository, err := sqlstore.NewStatisticsRepository(sqlstore.SQLiteDialect)
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := inference.NewTokenEstimatorProfile("character:weighted", "1")
	service, err := sqlstore.NewScopedStatistics(database, "scope-a", "memory", artifacts, repository, &profile)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC)
	input, output := int64(11), int64(7)
	if err := service.Record(ctx, stats.MemoryExtraction, stats.Generation,
		inference.Usage{Requests: 1, InputTokens: &input, OutputTokens: &output}, date); err != nil {
		t.Fatal(err)
	}
	output2 := int64(5)
	if err := service.Record(ctx, stats.MemoryExtraction, stats.Generation,
		inference.Usage{Requests: 2, OutputTokens: &output2}, date); err != nil {
		t.Fatal(err)
	}
	measurement, _ := stats.NewRecallTokenMeasurement(profile, true, true, 100, 40)
	if err := service.RecordRecall(ctx, measurement, date); err != nil {
		t.Fatal(err)
	}
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pc_source_journal_heads (scope_id, position) VALUES (?, ?)`, "scope-a", 5); err != nil {
			return err
		}
		cursor := sqlstore.SourceCursorRepository{}
		_, err := cursor.Save(ctx, tx, "scope-a", trigger.SourceWindowName, source.NewCursor(3), nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	overview, err := service.Overview(ctx, stats.Today, date.Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	sources := overview.Inventory().Sources()
	if sources.Total() != 5 || sources.MemoryProcessed() != 3 || sources.MemoryPending() != 2 {
		t.Fatalf("unexpected Source inventory: %#v", sources)
	}
	generation := overview.Usage().Totals().Generation()
	if generation.Requests() != 3 || generation.InputTokens() != nil || generation.OutputTokens() == nil || *generation.OutputTokens() != 12 {
		t.Fatalf("unexpected usage total: requests=%d input=%v output=%v", generation.Requests(), generation.InputTokens(), generation.OutputTokens())
	}
	recall := overview.Recall()
	if recall.Estimator() == nil || recall.Totals().TokenReduction() != 60 || len(recall.Daily()) != 1 {
		t.Fatalf("unexpected recall: %#v", recall)
	}
}

func externalRegistration(t *testing.T, name, locator, fingerprintPrefix string) skill.Registration {
	return externalRegistrationForProvider(t, "codex", name, locator, fingerprintPrefix)
}

func externalRegistrationForProvider(
	t *testing.T,
	provider, name, locator, fingerprintPrefix string,
) skill.Registration {
	t.Helper()
	fingerprint := fingerprintPrefix
	for len(fingerprint) < 64 {
		fingerprint += fingerprintPrefix
	}
	value, err := skill.NewRegistration(
		provider+":project:repository/"+name, provider, provider, "workstation-1",
		skill.ProjectScope, locator, fingerprint[:64], name, "Use "+name+" for a bounded task.",
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
