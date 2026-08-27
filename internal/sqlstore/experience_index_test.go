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

//go:build sqlite_fts5

package sqlstore_test

import (
	"context"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/experience"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
)

func TestSQLiteExperienceFTSTracksCurrentHeadsAndRebuilds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	_, artifacts := repositories(t)
	index := sqlstore.SQLiteExperienceFTSIndex{}
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		return index.Initialize(ctx, tx)
	}); err != nil {
		t.Fatal(err)
	}

	firstContent, err := experience.NewContent(
		"A generated client contains hamsterlegacy.", "Regenerate it.", "The contract agrees.", "Inspect the diff.",
	)
	if err != nil {
		t.Fatal(err)
	}
	firstDraft, err := experience.NewDraft(firstContent, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var first experience.Experience
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		created, createErr := artifacts.Create(ctx, tx, "project", "experience-1", firstDraft)
		if createErr != nil {
			return createErr
		}
		first = created.(artifact.Artifact[experience.Content])
		return index.Replace(ctx, tx, "project", first)
	}); err != nil {
		t.Fatal(err)
	}
	assertExperienceHits(t, database, index, "project", "hamsterlegacy", first.Ref())
	assertExperienceHits(t, database, index, "other-project", "hamsterlegacy")

	secondContent, err := experience.NewContent(
		"A generated client contains falconcurrent.", "Regenerate it.", "The contract agrees.", "Inspect the diff.",
	)
	if err != nil {
		t.Fatal(err)
	}
	secondDraft, err := experience.NewDraft(secondContent, nil, []artifact.Ref{first.Ref()})
	if err != nil {
		t.Fatal(err)
	}
	var second experience.Experience
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		revised, reviseErr := artifacts.Revise(ctx, tx, "project", first, secondDraft)
		if reviseErr != nil {
			return reviseErr
		}
		second = revised.(artifact.Artifact[experience.Content])
		return index.Replace(ctx, tx, "project", second)
	}); err != nil {
		t.Fatal(err)
	}
	assertExperienceHits(t, database, index, "project", "hamsterlegacy")
	assertExperienceHits(t, database, index, "project", "falconcurrent", second.Ref())

	if _, err := database.SQLDB().ExecContext(ctx, `UPDATE pc_artifact_heads
        SET searchable_text = NULL WHERE scope_id = ? AND family = ?`, "project", experience.Family); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQLDB().ExecContext(ctx, "DELETE FROM pc_experience_fts"); err != nil {
		t.Fatal(err)
	}
	assertExperienceHits(t, database, index, "project", "falconcurrent")
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		return index.Initialize(ctx, tx)
	}); err != nil {
		t.Fatal(err)
	}
	assertExperienceHits(t, database, index, "project", "falconcurrent", second.Ref())
}

func assertExperienceHits(
	t *testing.T,
	database *sqlstore.Database,
	index sqlstore.SQLiteExperienceFTSIndex,
	scopeID, query string,
	want ...artifact.Ref,
) {
	t.Helper()
	var hits []experience.SearchHit
	if err := database.Transaction(context.Background(), func(tx sqlstore.DBTX) error {
		var searchErr error
		hits, searchErr = index.Search(context.Background(), tx, scopeID, query, 8)
		return searchErr
	}); err != nil {
		t.Fatal(err)
	}
	if len(hits) != len(want) {
		t.Fatalf("hits for %q/%q = %#v, want %v", scopeID, query, hits, want)
	}
	for index := range want {
		if hits[index].ArtifactRef != want[index] {
			t.Fatalf("hit %d = %s, want %s", index, hits[index].ArtifactRef, want[index])
		}
	}
}
