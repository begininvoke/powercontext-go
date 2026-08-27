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

	"github.com/ob-labs/powercontext-go/artifact/skill"
	"github.com/ob-labs/powercontext-go/source"
)

type ExternalSkillSnapshotStore struct {
	database   *Database
	repository *SourceRepository
	adapter    skill.SnapshotSourceAdapter
}

func NewExternalSkillSnapshotStore(
	database *Database,
	repository *SourceRepository,
) (*ExternalSkillSnapshotStore, error) {
	if database == nil || repository == nil {
		return nil, errors.New("sqlstore: external Skill snapshot dependencies must not be nil")
	}
	return &ExternalSkillSnapshotStore{database: database, repository: repository}, nil
}

func (s *ExternalSkillSnapshotStore) Store(
	ctx context.Context,
	scopeID string,
	capture skill.SnapshotCapture,
) (source.Ref, error) {
	value, err := s.adapter.Resolve(ctx, capture)
	if err != nil {
		return source.Ref{}, err
	}
	var stored StoredSource
	err = s.database.Transaction(ctx, func(tx DBTX) error {
		var addErr error
		stored, addErr = s.repository.Add(ctx, tx, scopeID, value)
		return addErr
	})
	if err != nil {
		var conflict *StoredPayloadConflictError
		if errors.As(err, &conflict) {
			return source.Ref{}, &source.ConflictError{Field: "source_id", Value: value.SourceName()}
		}
		return source.Ref{}, err
	}
	return stored.Ref, nil
}
