package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/thunguo/powercontext-go/source"
)

const ExternalSnapshotSourceType = "external-skill-snapshot"

type ImportMode string

const (
	ImportModeImport ImportMode = "import"
	ImportModeFork   ImportMode = "fork"
)

type SnapshotCapture struct {
	Snapshot Snapshot
	Mode     ImportMode
}

type SnapshotSource struct {
	name     string
	snapshot Snapshot
	mode     ImportMode
}

func (s SnapshotSource) SourceName() string { return s.name }
func (SnapshotSource) SourceMaterialization() source.Materialization {
	return source.Captured
}
func (SnapshotSource) SourceDescription() (string, bool) {
	return "Exact external Skill snapshot captured by an explicit managed import or fork.", true
}
func (s SnapshotSource) Snapshot() Snapshot { return s.snapshot }
func (s SnapshotSource) Mode() ImportMode   { return s.mode }

type SnapshotSourceAdapter struct{}

func (SnapshotSourceAdapter) Name() string { return ExternalSnapshotSourceType }
func (SnapshotSourceAdapter) Resolve(_ context.Context, value SnapshotCapture) (SnapshotSource, error) {
	if value.Mode != ImportModeImport && value.Mode != ImportModeFork {
		return SnapshotSource{}, fmt.Errorf("invalid external Skill import mode %q", value.Mode)
	}
	registration := value.Snapshot.registration
	identity := registration.provider + "\x00" + registration.agentKind + "\x00" + registration.hostID + "\x00" +
		registration.externalSkillID + "\x00" + registration.fingerprint + "\x00" + string(value.Mode)
	digest := sha256.Sum256([]byte(identity))
	return SnapshotSource{
		name: "ext_skill_" + hex.EncodeToString(digest[:]), snapshot: value.Snapshot, mode: value.Mode,
	}, nil
}
func (SnapshotSourceAdapter) Read(_ context.Context, value SnapshotSource) (SnapshotCapture, error) {
	return SnapshotCapture{Snapshot: value.snapshot, Mode: value.mode}, nil
}

func RegisterSnapshotSource(registry *source.Registry) error {
	return source.Register(registry, SnapshotSourceAdapter{})
}
