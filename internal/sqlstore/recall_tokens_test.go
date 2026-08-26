package sqlstore_test

import (
	"context"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/experience"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/contextpack"
	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
	"github.com/ob-labs/powercontext-go/source"
)

func TestRelationalRecallTokenEstimatorResolvesMemoryAndRecursiveArtifactLineage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	sources, artifacts := repositories(t)
	first := contentSource(t, "short-a", "a", nil)
	second := contentSource(t, "short-b", "b", nil)
	content, err := experience.NewContent("situation", "action", "outcome", "lesson")
	if err != nil {
		t.Fatal(err)
	}

	var firstStored, secondStored sqlstore.StoredSource
	var child artifact.Snapshot
	err = database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var writeErr error
		firstStored, writeErr = sources.Add(ctx, tx, "scope-recall", first)
		if writeErr != nil {
			return writeErr
		}
		secondStored, writeErr = sources.Add(ctx, tx, "scope-recall", second)
		if writeErr != nil {
			return writeErr
		}
		parentDraft, writeErr := experience.NewDraft(content, []source.Ref{firstStored.Ref, secondStored.Ref}, nil)
		if writeErr != nil {
			return writeErr
		}
		parent, writeErr := artifacts.Create(ctx, tx, "scope-recall", "parent", parentDraft)
		if writeErr != nil {
			return writeErr
		}
		childDraft, writeErr := experience.NewDraft(
			content, []source.Ref{secondStored.Ref}, []artifact.Ref{parent.Ref()},
		)
		if writeErr != nil {
			return writeErr
		}
		child, writeErr = artifacts.Create(ctx, tx, "scope-recall", "child", childDraft)
		return writeErr
	})
	if err != nil {
		t.Fatal(err)
	}

	memoryRepository, err := sqlstore.NewMemoryRepository(database, "scope-recall", artifacts, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := sqlstore.NewMemorySourceResolver(database, "scope-recall", sources)
	if err != nil {
		t.Fatal(err)
	}
	service, err := memory.NewService(memoryRepository, memory.ServiceOptions{
		SourceResolver: resolver, IDFactory: sequentialMemoryIDs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := memory.NewEntryInput(
		nil, "fact", "remember both Sources", []source.Value{first, second}, nil, nil,
	)
	remembered, err := service.Remember(
		ctx, nil, []source.Value{first, second}, nil,
		[]memory.EntryInput{input}, memory.RememberAppend,
	)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := service.Entries(ctx, *remembered)
	if err != nil {
		t.Fatal(err)
	}

	request, err := contextpack.NewRequest("remember Sources", 0)
	if err != nil {
		t.Fatal(err)
	}
	build, err := (contextpack.Builder{}).BuildResult(
		request,
		refPointer(remembered.Ref()),
		[]memory.Hit{{
			MemoryRef: remembered.Ref(), EntryID: entries[0].EntryID,
			EntryVersionID: entries[0].EntryVersionID, Text: entries[0].Text, Score: 1,
		}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	estimator, err := sqlstore.NewRelationalRecallTokenEstimator(
		database, "scope-recall", sources, artifacts, inference.CharacterTokenEstimator(),
	)
	if err != nil {
		t.Fatal(err)
	}
	measurement, err := estimator.Estimate(ctx, build)
	if err != nil {
		t.Fatal(err)
	}
	if !measurement.Ready() || !measurement.Comparable() {
		t.Fatalf("measurement readiness = (%t, %t)", measurement.Ready(), measurement.Comparable())
	}
	// Each complete Source is estimated independently (1 + 1), while the
	// duplicated second Source is de-duplicated across direct and recursive
	// lineage.
	if measurement.BaselineTokens() != 2 {
		t.Fatalf("baseline tokens = %d, want 2", measurement.BaselineTokens())
	}
	if measurement.RecalledTokens() <= 0 {
		t.Fatalf("recalled tokens = %d, want positive", measurement.RecalledTokens())
	}

	artifactBuild, err := (contextpack.Builder{}).BuildResult(
		request,
		nil,
		nil,
		[]experience.SearchHit{{ArtifactRef: child.Ref(), Content: content}},
	)
	if err != nil {
		t.Fatal(err)
	}
	artifactMeasurement, err := estimator.Estimate(ctx, artifactBuild)
	if err != nil {
		t.Fatal(err)
	}
	if artifactMeasurement.BaselineTokens() != 2 {
		t.Fatalf("recursive Artifact baseline = %d, want 2", artifactMeasurement.BaselineTokens())
	}
}

func refPointer(value artifact.Ref) *artifact.Ref { return &value }
