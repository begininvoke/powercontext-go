package handoffreport

import (
	"encoding/json"

	"github.com/ob-labs/powercontext-go/artifact"
)

type SelectionEntry struct {
	scopeID            string
	workstreamRevision int
	status             SelectionStatus
	handoffRef         *artifact.Ref
}

func NewSelectionEntry(scopeID string, revision int, status SelectionStatus, ref *artifact.Ref) (SelectionEntry, error) {
	value := SelectionEntry{scopeID, revision, status, cloneArtifactRef(ref)}
	if err := value.Validate(); err != nil {
		return SelectionEntry{}, err
	}
	return value, nil
}
func (v SelectionEntry) Validate() error {
	if err := requireText("scope_id", v.scopeID, MaxScopeIDLength); err != nil {
		return err
	}
	if v.workstreamRevision < 1 {
		return fieldError("workstream_revision", "must be positive")
	}
	if v.status == SelectionSelected {
		if v.handoffRef == nil || v.handoffRef.Validate() != nil || v.handoffRef.Family() != "handoff" {
			return fieldError("handoff_ref", "must be an exact Handoff reference")
		}
	} else if v.status == SelectionNoHandoff {
		if v.handoffRef != nil {
			return fieldError("handoff_ref", "must be null for no_handoff")
		}
	} else {
		return fieldError("status", "has an unsupported value")
	}
	return nil
}
func (v SelectionEntry) ScopeID() string           { return v.scopeID }
func (v SelectionEntry) WorkstreamRevision() int   { return v.workstreamRevision }
func (v SelectionEntry) Status() SelectionStatus   { return v.status }
func (v SelectionEntry) HandoffRef() *artifact.Ref { return cloneArtifactRef(v.handoffRef) }
func (v SelectionEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"scope_id": v.scopeID, "workstream_revision": v.workstreamRevision, "status": v.status, "handoff_ref": artifactRefMap(v.handoffRef)})
}
