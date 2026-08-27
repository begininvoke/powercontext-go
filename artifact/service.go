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

package artifact

import "context"

type Catalog[T any] interface {
	Get(context.Context, Artifact[T]) (Artifact[T], error)
	Latest(context.Context, Artifact[T]) (Artifact[T], error)
	Revisions(context.Context, Artifact[T]) ([]Artifact[T], error)
}

type Store[T any] interface {
	Add(context.Context, Draft[T]) (Artifact[T], error)
	Revise(context.Context, Artifact[T], Draft[T]) (Artifact[T], error)
}

type Service[T any] struct {
	catalog Catalog[T]
	store   Store[T]
}

func NewService[T any](catalog Catalog[T], store Store[T]) *Service[T] {
	return &Service[T]{catalog: catalog, store: store}
}

func (s *Service[T]) Add(ctx context.Context, draft Draft[T]) (Artifact[T], error) {
	return s.store.Add(ctx, draft)
}

func (s *Service[T]) Revise(ctx context.Context, current Artifact[T], draft Draft[T]) (Artifact[T], error) {
	if current.Family() != draft.Family() {
		return Artifact[T]{}, &FamilyMismatchError{
			ArtifactFamily: current.Family(),
			DraftFamily:    draft.Family(),
		}
	}
	return s.store.Revise(ctx, current, draft)
}

func (s *Service[T]) Get(ctx context.Context, value Artifact[T]) (Artifact[T], error) {
	return s.catalog.Get(ctx, value)
}

func (s *Service[T]) Latest(ctx context.Context, value Artifact[T]) (Artifact[T], error) {
	return s.catalog.Latest(ctx, value)
}

func (s *Service[T]) Revisions(ctx context.Context, value Artifact[T]) ([]Artifact[T], error) {
	return s.catalog.Revisions(ctx, value)
}
