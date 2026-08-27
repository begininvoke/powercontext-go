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

package conformance_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ob-labs/powercontext-go/artifact/experience"
	"github.com/ob-labs/powercontext-go/internal/review"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
)

const (
	reviewCompatibilityScope = "project:review-compatibility"
	pythonCandidateID        = "candidate-python"
	postGoCandidateID        = "candidate-python-after-go"
)

func TestPythonGoPythonReviewDatabaseCompatibility(t *testing.T) {
	python := os.Getenv("POWERCONTEXT_ORACLE_PYTHON")
	if python == "" {
		t.Skip("POWERCONTEXT_ORACLE_PYTHON is unset; bidirectional Review compatibility runs in the Oracle CI job")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	databasePath := filepath.Join(t.TempDir(), "review.db")
	runPythonReviewFixture(t, ctx, python, "create", databasePath)

	database, sources, artifacts, candidates := openReviewCompatibilityDatabase(t, ctx, databasePath)
	backend, err := sqlstore.NewReviewBackend(
		database, reviewCompatibilityScope, candidates, artifacts, sources, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	service, err := review.NewService(backend, func(kind string) (string, error) {
		if kind == experience.Family {
			return "experience-go", nil
		}
		return "unused-" + kind, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	current, err := service.Get(ctx, pythonCandidateID)
	if err != nil {
		t.Fatal(err)
	}
	proposal, ok := current.ProposalValue().(experience.Content)
	if !ok || proposal.Lesson() != "Python initial lesson." || current.Reason() == nil ||
		*current.Reason() != "Created by Python café." || len(current.Sources()) != 1 {
		t.Fatalf("Go read of Python Candidate = %#v", current)
	}
	revisedContent, err := experience.NewContent(
		"The public contract changed.",
		"Regenerate the client and run contract tests.",
		"The transport stayed aligned.",
		"Go reviewed lesson.",
	)
	if err != nil {
		t.Fatal(err)
	}
	reason := "Reviewed by Go café."
	revised, err := service.Revise(
		ctx, pythonCandidateID, 1, revisedContent, current.Sources(), nil, nil, &reason,
	)
	if err != nil {
		t.Fatal(err)
	}
	if revised.Version() != 2 {
		t.Fatalf("Go revision = %#v", revised)
	}
	approved, err := service.Approve(ctx, pythonCandidateID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status() != review.Approved || approved.ResultArtifact() == nil ||
		approved.ResultArtifact().ID() != "experience-go" {
		t.Fatalf("Go approval = %#v", approved)
	}
	if err := database.Close(ctx); err != nil {
		t.Fatal(err)
	}

	runPythonReviewFixture(t, ctx, python, "verify", databasePath)

	database, _, _, candidates = openReviewCompatibilityDatabase(t, ctx, databasePath)
	defer func() {
		if err := database.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	var appended review.Snapshot
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var getErr error
		appended, getErr = candidates.Get(ctx, tx, reviewCompatibilityScope, postGoCandidateID)
		return getErr
	}); err != nil {
		t.Fatal(err)
	}
	postGoProposal, ok := appended.ProposalValue().(experience.Content)
	if !ok || postGoProposal.Lesson() != "Python still writes after Go." || appended.Status() != review.Pending {
		t.Fatalf("Go read after Python back-read/write = %#v", appended)
	}
}

func openReviewCompatibilityDatabase(
	t *testing.T,
	ctx context.Context,
	databasePath string,
) (*sqlstore.Database, *sqlstore.SourceRepository, *sqlstore.ArtifactRepository, *sqlstore.CandidateRepository) {
	t.Helper()
	database, err := sqlstore.OpenSQLite(ctx, sqlstore.DefaultSQLiteConfig(databasePath))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := sqlstore.NewSourceRepository(sqlstore.SQLiteDialect, sqlstore.ContentSourceCodec())
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := sqlstore.NewArtifactRepository(
		sqlstore.SQLiteDialect, sqlstore.ExperienceArtifactCodec(), sqlstore.SkillArtifactCodec(),
	)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := sqlstore.NewCandidateRepository(
		sqlstore.SQLiteDialect, sqlstore.ExperienceArtifactCodec(), sqlstore.SkillArtifactCodec(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return database, sources, artifacts, candidates
}

func runPythonReviewFixture(
	t *testing.T,
	ctx context.Context,
	python, mode, databasePath string,
) {
	t.Helper()
	command := exec.CommandContext(ctx, python, "python_review_fixture.py", mode, databasePath)
	command.Dir = "."
	output, err := command.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("Python Review fixture %s timed out: %v\n%s", mode, err, output)
		}
		t.Fatalf("Python Review fixture %s failed: %v\n%s", mode, err, output)
	}
}
