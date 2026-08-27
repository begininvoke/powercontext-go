package endpoint

import (
	"context"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/artifact/skill"
	"github.com/ob-labs/powercontext-go/internal/review"
	"github.com/ob-labs/powercontext-go/internal/runtime"
)

type ExternalSkillOperations interface {
	Scan(context.Context, string) (skill.ProviderScan, error)
	List(context.Context, string, bool) ([]skill.Resolution, error)
	Resolve(context.Context, string, string, string) (skill.Resolution, error)
	Import(context.Context, string, string, string, skill.ImportMode, *string) (review.GeneratedCandidateResult, error)
}

func (h *Handler) ScanExternalSkills(
	ctx context.Context,
	req *v1.ScanExternalSkillsRequest,
) (v1.ScanExternalSkillsRes, error) {
	if h.external == nil {
		return nil, &skill.ExternalRegistryUnavailableError{}
	}
	result, err := h.external.Scan(ctx, req.ScopeID)
	if err != nil {
		return nil, err
	}
	registrations := result.Registrations()
	wire := make([]v1.ExternalSkillRegistration, len(registrations))
	for index, registration := range registrations {
		wire[index] = externalSkillRegistration(registration)
	}
	return &v1.ScanExternalSkillsResponseHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response:               v1.ScanExternalSkillsResponse{Registrations: wire, Skipped: result.Skipped()},
	}, nil
}

func (h *Handler) ListExternalSkills(
	ctx context.Context,
	req *v1.ListExternalSkillsRequest,
) (v1.ListExternalSkillsRes, error) {
	if h.external == nil {
		return nil, &skill.ExternalRegistryUnavailableError{}
	}
	result, err := h.external.List(ctx, req.ScopeID, req.IncludeUnavailable.Or(false))
	if err != nil {
		return nil, err
	}
	wire := make([]v1.ExternalSkillResolution, len(result))
	for index, value := range result {
		wire[index] = externalSkillResolution(value)
	}
	return &v1.ListExternalSkillsResponseHeaders{
		XPowerContextRequestID: requestID(ctx), Response: v1.ListExternalSkillsResponse{Skills: wire},
	}, nil
}

func (h *Handler) ResolveExternalSkill(
	ctx context.Context,
	req *v1.ResolveExternalSkillRequest,
) (v1.ResolveExternalSkillRes, error) {
	if h.external == nil {
		return nil, &skill.ExternalRegistryUnavailableError{}
	}
	result, err := h.external.Resolve(ctx, req.ScopeID, req.ExternalSkillID, req.Fingerprint)
	if err != nil {
		return nil, err
	}
	return &v1.ExternalSkillResolutionHeaders{
		XPowerContextRequestID: requestID(ctx), Response: externalSkillResolution(result),
	}, nil
}

func (h *Handler) ImportExternalSkill(
	ctx context.Context,
	req *v1.ImportExternalSkillRequest,
) (v1.ImportExternalSkillRes, error) {
	if h.external == nil {
		return nil, &skill.ExternalRegistryUnavailableError{}
	}
	result, err := h.external.Import(
		ctx, req.ScopeID, req.ExternalSkillID, req.Fingerprint,
		skill.ImportMode(req.Mode), optionalString(req.Reason),
	)
	if err != nil {
		return nil, err
	}
	return generatedCandidateHeaders(ctx, result)
}

func externalSkillRegistration(value skill.Registration) v1.ExternalSkillRegistration {
	return v1.ExternalSkillRegistration{
		ExternalSkillID: value.ExternalSkillID(),
		Provider:        v1.ExternalSkillRegistrationProvider(value.Provider()),
		AgentKind:       v1.ExternalSkillRegistrationAgentKind(value.AgentKind()),
		HostID:          value.HostID(),
		InstallationScope: v1.ExternalSkillInstallationScope(
			value.InstallationScope(),
		),
		Locator: value.Locator(), Fingerprint: value.Fingerprint(),
		Name: value.Name(), Description: value.Description(),
	}
}

func externalSkillResolution(value skill.Resolution) v1.ExternalSkillResolution {
	var entrypoint *string
	if value.Entrypoint != "" {
		entrypoint = &value.Entrypoint
	}
	return v1.ExternalSkillResolution{
		Registration: externalSkillRegistration(value.Registration),
		Status:       v1.ExternalSkillResolutionStatus(value.Status),
		Entrypoint:   nullableString(entrypoint),
	}
}

var _ ExternalSkillOperations = (*runtime.ExternalSkillApplication)(nil)
