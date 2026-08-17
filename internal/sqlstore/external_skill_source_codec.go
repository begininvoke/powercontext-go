package sqlstore

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/thunguo/powercontext-go/artifact/skill"
	"github.com/thunguo/powercontext-go/source"
)

// ExternalSkillSnapshotSourceCodec preserves the frozen Pydantic Source JSON
// used by explicit external Skill import/fork evidence.
func ExternalSkillSnapshotSourceCodec() SourceCodec {
	codec, err := NewSourceCodec(
		skill.ExternalSnapshotSourceType,
		encodeExternalSkillSnapshotSource,
		decodeExternalSkillSnapshotSource,
	)
	if err != nil {
		panic(err)
	}
	return codec
}

type externalSkillRegistrationJSON struct {
	ExternalSkillID   string                  `json:"external_skill_id"`
	Provider          string                  `json:"provider"`
	AgentKind         string                  `json:"agent_kind"`
	HostID            string                  `json:"host_id"`
	InstallationScope skill.InstallationScope `json:"installation_scope"`
	Locator           string                  `json:"locator"`
	Fingerprint       string                  `json:"fingerprint"`
	Name              string                  `json:"name"`
	Description       string                  `json:"description"`
}

type externalSkillSnapshotJSON struct {
	Registration externalSkillRegistrationJSON `json:"registration"`
	Manifest     string                        `json:"manifest"`
}

type externalSkillSnapshotSourceJSON struct {
	Name            string                    `json:"name"`
	Materialization source.Materialization    `json:"materialization"`
	Description     *string                   `json:"description"`
	Snapshot        externalSkillSnapshotJSON `json:"snapshot"`
	Mode            skill.ImportMode          `json:"mode"`
}

func encodeExternalSkillSnapshotSource(value skill.SnapshotSource) ([]byte, error) {
	description, present := value.SourceDescription()
	if !present {
		return nil, fmt.Errorf("external Skill snapshot Source description is missing")
	}
	return marshalJSON(externalSkillSnapshotSourceJSON{
		Name: value.SourceName(), Materialization: value.SourceMaterialization(), Description: &description,
		Snapshot: externalSkillSnapshotJSON{
			Registration: externalSkillRegistrationFields(value.Snapshot().Registration()),
			Manifest:     value.Snapshot().Manifest(),
		},
		Mode: value.Mode(),
	})
}

func decodeExternalSkillSnapshotSource(payload []byte) (skill.SnapshotSource, error) {
	var stored externalSkillSnapshotSourceJSON
	if err := unmarshalJSON(payload, &stored); err != nil {
		return skill.SnapshotSource{}, err
	}
	if !utf8.ValidString(stored.Snapshot.Manifest) {
		return skill.SnapshotSource{}, fmt.Errorf("external Skill manifest is not valid UTF-8")
	}
	registration, err := externalSkillRegistration(stored.Snapshot.Registration)
	if err != nil {
		return skill.SnapshotSource{}, err
	}
	snapshot, err := skill.NewSnapshot(registration, stored.Snapshot.Manifest)
	if err != nil {
		return skill.SnapshotSource{}, err
	}
	resolved, err := (skill.SnapshotSourceAdapter{}).Resolve(
		context.Background(), skill.SnapshotCapture{Snapshot: snapshot, Mode: stored.Mode},
	)
	if err != nil {
		return skill.SnapshotSource{}, err
	}
	description, _ := resolved.SourceDescription()
	if stored.Name != resolved.SourceName() || stored.Materialization != source.Captured ||
		stored.Description == nil || *stored.Description != description {
		return skill.SnapshotSource{}, fmt.Errorf("external Skill snapshot Source authority fields are inconsistent")
	}
	return resolved, nil
}

func externalSkillRegistrationFields(value skill.Registration) externalSkillRegistrationJSON {
	return externalSkillRegistrationJSON{
		ExternalSkillID: value.ExternalSkillID(), Provider: value.Provider(), AgentKind: value.AgentKind(),
		HostID: value.HostID(), InstallationScope: value.InstallationScope(), Locator: value.Locator(),
		Fingerprint: value.Fingerprint(), Name: value.Name(), Description: value.Description(),
	}
}

func externalSkillRegistration(value externalSkillRegistrationJSON) (skill.Registration, error) {
	return skill.NewRegistration(
		value.ExternalSkillID, value.Provider, value.AgentKind, value.HostID,
		value.InstallationScope, value.Locator, value.Fingerprint, value.Name, value.Description,
	)
}
