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

package sqlstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
)

// HandoffBackend binds one scoped Handoff lifecycle to the shared immutable
// Artifact tables. Generation and evidence validation remain outside this
// adapter; only the final Artifact CAS is transactional here.
type HandoffBackend struct {
	database  *Database
	scopeID   string
	artifacts *ArtifactRepository
}

func NewHandoffBackend(database *Database, scopeID string, artifacts *ArtifactRepository) (*HandoffBackend, error) {
	if database == nil || artifacts == nil {
		return nil, errors.New("sqlstore: Handoff persistence dependencies must not be nil")
	}
	if err := requireScope(scopeID); err != nil {
		return nil, err
	}
	return &HandoffBackend{database: database, scopeID: scopeID, artifacts: artifacts}, nil
}

func (b *HandoffBackend) Create(
	ctx context.Context,
	artifactID string,
	draft handoff.ArtifactDraft,
) (handoff.Handoff, error) {
	var stored artifact.Snapshot
	err := b.database.Transaction(ctx, func(tx DBTX) error {
		var err error
		stored, err = b.artifacts.Create(ctx, tx, b.scopeID, artifactID, draft)
		return err
	})
	if err != nil {
		return handoff.Handoff{}, err
	}
	return storedHandoff(stored)
}

func (b *HandoffBackend) Revise(
	ctx context.Context,
	base handoff.Handoff,
	draft handoff.ArtifactDraft,
) (handoff.Handoff, error) {
	var stored artifact.Snapshot
	err := b.database.Transaction(ctx, func(tx DBTX) error {
		var err error
		stored, err = b.artifacts.Revise(ctx, tx, b.scopeID, base, draft)
		return err
	})
	if err != nil {
		return handoff.Handoff{}, err
	}
	return storedHandoff(stored)
}

func (b *HandoffBackend) Get(ctx context.Context, ref artifact.Ref) (handoff.Handoff, error) {
	var stored artifact.Snapshot
	err := b.database.Transaction(ctx, func(tx DBTX) error {
		var err error
		stored, err = b.artifacts.Get(ctx, tx, b.scopeID, ref)
		return err
	})
	if err != nil {
		var missing *RepositoryNotFoundError
		if errors.As(err, &missing) {
			return handoff.Handoff{}, &artifact.NotFoundError{Ref: ref}
		}
		return handoff.Handoff{}, err
	}
	return storedHandoff(stored)
}

func (b *HandoffBackend) Latest(ctx context.Context, artifactID string) (handoff.Handoff, bool, error) {
	var stored artifact.Snapshot
	err := b.database.Transaction(ctx, func(tx DBTX) error {
		var err error
		stored, err = b.artifacts.Latest(ctx, tx, b.scopeID, handoff.Family, artifactID)
		return err
	})
	if err != nil {
		var missing *RepositoryNotFoundError
		if errors.As(err, &missing) {
			return handoff.Handoff{}, false, nil
		}
		return handoff.Handoff{}, false, err
	}
	value, err := storedHandoff(stored)
	return value, err == nil, err
}

func (b *HandoffBackend) Revisions(ctx context.Context, artifactID string) ([]handoff.Handoff, error) {
	var stored []artifact.Snapshot
	err := b.database.Transaction(ctx, func(tx DBTX) error {
		var err error
		stored, err = b.artifacts.Revisions(ctx, tx, b.scopeID, handoff.Family, artifactID)
		return err
	})
	if err != nil {
		return nil, err
	}
	result := make([]handoff.Handoff, len(stored))
	for index, snapshot := range stored {
		value, err := storedHandoff(snapshot)
		if err != nil {
			return nil, err
		}
		result[index] = value
	}
	return result, nil
}

func storedHandoff(snapshot artifact.Snapshot) (handoff.Handoff, error) {
	value, ok := snapshot.(artifact.Artifact[handoff.Content])
	if !ok {
		return handoff.Handoff{}, fmt.Errorf("sqlstore: Handoff codec returned %T", snapshot)
	}
	return value, nil
}
