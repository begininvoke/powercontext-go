package runtime

import (
	"context"
	"errors"

	"github.com/ob-labs/powercontext-go/artifact/skill"
	"github.com/ob-labs/powercontext-go/internal/review"
	"github.com/ob-labs/powercontext-go/internal/stats"
	"github.com/ob-labs/powercontext-go/source"
)

type ExternalSkillRegistryFactory func(string) (*skill.RegistryService, error)

type ExternalSkillSnapshotStore interface {
	Store(context.Context, string, skill.SnapshotCapture) (source.Ref, error)
}

type ExternalSkillApplication struct {
	runtime     *Runtime
	registries  ExternalSkillRegistryFactory
	generations GenerationServiceFactory
	snapshots   ExternalSkillSnapshotStore
}

func NewExternalSkillApplication(
	runtime *Runtime,
	registries ExternalSkillRegistryFactory,
	generations GenerationServiceFactory,
	snapshots ExternalSkillSnapshotStore,
) (*ExternalSkillApplication, error) {
	if runtime == nil {
		return nil, errors.New("runtime: external Skill Runtime must not be nil")
	}
	return &ExternalSkillApplication{
		runtime: runtime, registries: registries, generations: generations, snapshots: snapshots,
	}, nil
}

func (a *ExternalSkillApplication) Scan(ctx context.Context, scopeID string) (skill.ProviderScan, error) {
	var result skill.ProviderScan
	err := a.runtime.ScopedWrite(ctx, scopeID, func(ctx context.Context, scope string) error {
		registry, err := a.registry(scope)
		if err != nil {
			return err
		}
		result, err = registry.Scan(ctx)
		return err
	})
	return result, err
}

func (a *ExternalSkillApplication) List(
	ctx context.Context,
	scopeID string,
	includeUnavailable bool,
) ([]skill.Resolution, error) {
	var result []skill.Resolution
	err := a.runtime.ScopedRead(ctx, scopeID, func(ctx context.Context, scope string) error {
		registry, err := a.registry(scope)
		if err != nil {
			return err
		}
		result, err = registry.List(ctx, includeUnavailable)
		return err
	})
	return append([]skill.Resolution(nil), result...), err
}

func (a *ExternalSkillApplication) Resolve(
	ctx context.Context,
	scopeID, externalSkillID, fingerprint string,
) (skill.Resolution, error) {
	var result skill.Resolution
	err := a.runtime.ScopedRead(ctx, scopeID, func(ctx context.Context, scope string) error {
		registry, err := a.registry(scope)
		if err != nil {
			return err
		}
		result, err = registry.Resolve(ctx, externalSkillID, fingerprint)
		return err
	})
	return result, err
}

func (a *ExternalSkillApplication) Import(
	ctx context.Context,
	scopeID, externalSkillID, fingerprint string,
	mode skill.ImportMode,
	reason *string,
) (review.GeneratedCandidateResult, error) {
	var result review.GeneratedCandidateResult
	err := a.runtime.ScopedWrite(ctx, scopeID, func(ctx context.Context, scope string) error {
		ctx = a.runtime.withModelUsage(ctx, scope, stats.SkillGeneration, "")
		generation, err := a.skillGeneration(scope)
		if err != nil {
			return err
		}
		// Capability is checked before touching host-local package content.
		if !generation.CanGenerateSkill() {
			return &review.GenerationCapabilityUnavailableError{Family: skill.Family}
		}
		registry, err := a.registry(scope)
		if err != nil {
			return err
		}
		snapshot, err := registry.Snapshot(ctx, externalSkillID, fingerprint)
		if err != nil {
			return err
		}
		if a.snapshots == nil {
			return &StateError{Code: "external-skill-import"}
		}
		ref, err := a.snapshots.Store(ctx, scope, skill.SnapshotCapture{Snapshot: snapshot, Mode: mode})
		if err != nil {
			return err
		}
		result, err = generation.Skill(ctx, review.SkillOriginSource, []source.Ref{ref}, nil, nil, reason)
		return err
	})
	return result, err
}

func (a *ExternalSkillApplication) registry(scope string) (*skill.RegistryService, error) {
	if a.registries == nil {
		return nil, &skill.ExternalRegistryUnavailableError{}
	}
	registry, err := a.registries(scope)
	if err != nil {
		return nil, err
	}
	if registry == nil {
		return nil, &skill.ExternalRegistryUnavailableError{}
	}
	return registry, nil
}

func (a *ExternalSkillApplication) skillGeneration(scope string) (*review.GenerationService, error) {
	if a.generations == nil {
		return nil, &review.GenerationCapabilityUnavailableError{Family: skill.Family}
	}
	service, err := a.generations(scope)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, &review.GenerationCapabilityUnavailableError{Family: skill.Family}
	}
	return service, nil
}
