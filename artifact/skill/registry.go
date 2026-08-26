package skill

import (
	"context"
	"fmt"
)

// RegistrationStore is the consumer-shaped persistence boundary for one
// scoped, rebuildable external Skill projection.
type RegistrationStore interface {
	Replace(context.Context, []string, string, []Registration) ([]Registration, error)
	Get(context.Context, string) (Registration, error)
	List(context.Context) ([]Registration, error)
}

// RegistryService discovers host-native Skills without assuming ownership of
// their package content. Exact content is captured only after fingerprint and
// live-provider revalidation.
type RegistryService struct {
	store    RegistrationStore
	provider ExternalProvider
}

func NewRegistryService(store RegistrationStore, provider ExternalProvider) (*RegistryService, error) {
	if store == nil || provider == nil {
		return nil, fmt.Errorf("external Skill Registry dependencies must be configured")
	}
	if err := externalText("provider", provider.Name(), 128); err != nil {
		return nil, err
	}
	if err := externalText("agent_kind", provider.AgentKind(), 128); err != nil {
		return nil, err
	}
	if err := externalText("host_id", provider.HostID(), MaxExternalHostIDLength); err != nil {
		return nil, err
	}
	providerNames := provider.ProviderNames()
	if len(providerNames) == 0 {
		return nil, fmt.Errorf("external Skill provider names must not be empty")
	}
	seen := make(map[string]struct{}, len(providerNames))
	for _, name := range providerNames {
		if !validAgentKind(name) {
			return nil, fmt.Errorf("invalid external Skill provider name %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("external Skill provider names must be unique")
		}
		seen[name] = struct{}{}
	}
	return &RegistryService{store: store, provider: provider}, nil
}

func (s *RegistryService) Scan(ctx context.Context) (ProviderScan, error) {
	snapshot, err := s.provider.Scan(ctx)
	if err != nil {
		return ProviderScan{}, err
	}
	if _, err := s.store.Replace(
		ctx, s.provider.ProviderNames(), s.provider.HostID(), snapshot.Registrations(),
	); err != nil {
		return ProviderScan{}, err
	}
	return snapshot, nil
}

func (s *RegistryService) List(ctx context.Context, includeUnavailable bool) ([]Resolution, error) {
	registrations, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Resolution, 0, len(registrations))
	for _, registration := range registrations {
		resolution, err := s.provider.Resolve(ctx, registration)
		if err != nil {
			return nil, err
		}
		if includeUnavailable || resolution.Status == Available {
			result = append(result, resolution)
		}
	}
	return result, nil
}

func (s *RegistryService) Resolve(
	ctx context.Context,
	externalSkillID, fingerprint string,
) (Resolution, error) {
	registration, err := s.store.Get(ctx, externalSkillID)
	if err != nil {
		return Resolution{}, err
	}
	if registration.Fingerprint() != fingerprint {
		return unavailable(registration), nil
	}
	return s.provider.Resolve(ctx, registration)
}

func (s *RegistryService) Snapshot(
	ctx context.Context,
	externalSkillID, fingerprint string,
) (Snapshot, error) {
	resolution, err := s.Resolve(ctx, externalSkillID, fingerprint)
	if err != nil {
		return Snapshot{}, err
	}
	if resolution.Status != Available || resolution.Entrypoint == "" {
		return Snapshot{}, &ExternalSnapshotUnavailableError{ExternalSkillID: externalSkillID}
	}
	manifest, err := readManifest(resolution.Entrypoint)
	if err != nil {
		return Snapshot{}, &ExternalSnapshotUnavailableError{ExternalSkillID: externalSkillID}
	}
	confirmed, err := s.Resolve(ctx, externalSkillID, fingerprint)
	if err != nil {
		return Snapshot{}, err
	}
	if confirmed.Status != Available || confirmed.Entrypoint != resolution.Entrypoint {
		return Snapshot{}, &ExternalSnapshotUnavailableError{ExternalSkillID: externalSkillID}
	}
	return NewSnapshot(resolution.Registration, manifest)
}
