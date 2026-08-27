package server

import (
	"context"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/skill"
	"github.com/ob-labs/powercontext-go/internal/review"
	pcruntime "github.com/ob-labs/powercontext-go/internal/runtime"
)

type webSkillProjectionOperations struct {
	review   *pcruntime.ReviewApplication
	external *pcruntime.ExternalSkillApplication
}

func (o webSkillProjectionOperations) GetCandidate(
	ctx context.Context,
	scopeID, candidateID string,
) (review.Snapshot, error) {
	if o.review == nil {
		return nil, &pcruntime.StateError{Code: "closed"}
	}
	return o.review.GetCandidate(ctx, scopeID, candidateID)
}

func (o webSkillProjectionOperations) GetSkill(
	ctx context.Context,
	scopeID string,
	ref artifact.Ref,
) (skill.Skill, error) {
	if o.review == nil {
		return skill.Skill{}, &pcruntime.StateError{Code: "closed"}
	}
	return o.review.GetSkill(ctx, scopeID, ref)
}

func (o webSkillProjectionOperations) ListExternalSkills(
	ctx context.Context,
	scopeID string,
	includeUnavailable bool,
) ([]skill.Resolution, error) {
	if o.external == nil {
		return nil, &skill.ExternalRegistryUnavailableError{}
	}
	return o.external.List(ctx, scopeID, includeUnavailable)
}

func (o webSkillProjectionOperations) ScanExternalSkills(
	ctx context.Context,
	scopeID string,
) (skill.ProviderScan, error) {
	if o.external == nil {
		return skill.ProviderScan{}, &skill.ExternalRegistryUnavailableError{}
	}
	return o.external.Scan(ctx, scopeID)
}
