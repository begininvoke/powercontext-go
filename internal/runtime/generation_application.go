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

package runtime

import (
	"context"
	"errors"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/internal/review"
	"github.com/ob-labs/powercontext-go/internal/stats"
	"github.com/ob-labs/powercontext-go/source"
)

type GenerationServiceFactory func(string) (*review.GenerationService, error)

type GenerationApplication struct {
	runtime  *Runtime
	services GenerationServiceFactory
}

func NewGenerationApplication(runtime *Runtime, services GenerationServiceFactory) (*GenerationApplication, error) {
	if runtime == nil || services == nil {
		return nil, errors.New("runtime: Generation application dependencies must not be nil")
	}
	return &GenerationApplication{runtime: runtime, services: services}, nil
}

func (a *GenerationApplication) GenerateExperience(
	ctx context.Context,
	scopeID string,
	sources []source.Ref,
	artifacts []artifact.Ref,
	target *artifact.Ref,
	reason *string,
) (review.GeneratedCandidateResult, error) {
	var result review.GeneratedCandidateResult
	err := a.runtime.ScopedWrite(ctx, scopeID, func(ctx context.Context, scope string) error {
		ctx = a.runtime.withModelUsage(ctx, scope, stats.ExperienceGeneration, "")
		service, err := a.service(scope)
		if err != nil {
			return err
		}
		result, err = service.Experience(ctx, sources, artifacts, target, reason)
		return err
	})
	return result, err
}

func (a *GenerationApplication) GenerateSkill(
	ctx context.Context,
	scopeID string,
	origin review.SkillGenerationOrigin,
	sources []source.Ref,
	artifacts []artifact.Ref,
	target *artifact.Ref,
	reason *string,
) (review.GeneratedCandidateResult, error) {
	var result review.GeneratedCandidateResult
	err := a.runtime.ScopedWrite(ctx, scopeID, func(ctx context.Context, scope string) error {
		ctx = a.runtime.withModelUsage(ctx, scope, stats.SkillGeneration, "")
		service, err := a.service(scope)
		if err != nil {
			return err
		}
		result, err = service.Skill(ctx, origin, sources, artifacts, target, reason)
		return err
	})
	return result, err
}

func (a *GenerationApplication) service(scope string) (*review.GenerationService, error) {
	service, err := a.services(scope)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, &StateError{Code: "generation"}
	}
	return service, nil
}
