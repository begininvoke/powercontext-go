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
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
)

func TestSQLiteMemoryFTSIndexRebuildAndSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	_, artifacts := repositories(t)
	repository, err := sqlstore.NewMemoryRepository(
		database,
		"scope-memory",
		artifacts,
		sqlstore.SQLiteMemoryFTSIndex{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	commit := initialMemoryCommit(t, "memory-fts")
	committed, err := repository.Commit(ctx, commit)
	if err != nil {
		t.Fatal(err)
	}
	// Initialize again after authoritative data exists to exercise the full rebuild.
	if err := repository.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	channels, err := repository.Search(ctx, memory.SearchRequest{
		Query:          "remember",
		AnalyzedQuery:  memory.AnalyzeText("remember"),
		Memories:       []artifact.Ref{committed.Ref()},
		CandidateLimit: 32,
		Mode:           memory.SearchFTS,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(channels.FTS) != 1 || channels.FTS[0].EntryID != "entry-1" {
		t.Fatalf("FTS hits = %#v", channels.FTS)
	}
}
