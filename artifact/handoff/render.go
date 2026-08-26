package handoff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/source"
)

type sourceRefDTO struct {
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
}

type artifactRefDTO struct {
	Family     string `json:"family"`
	ArtifactID string `json:"artifact_id"`
	Revision   int64  `json:"revision"`
}

type memoryCitationDTO struct {
	MemoryRef      artifactRefDTO `json:"memory_ref"`
	EntryID        string         `json:"entry_id"`
	EntryVersionID string         `json:"entry_version_id"`
}

type sourceCitationDTO struct {
	Kind      string       `json:"kind"`
	SourceRef sourceRefDTO `json:"source_ref"`
}

type artifactCitationDTO struct {
	Kind        string         `json:"kind"`
	ArtifactRef artifactRefDTO `json:"artifact_ref"`
}

type handoffMemoryCitationDTO struct {
	Kind           string            `json:"kind"`
	MemoryCitation memoryCitationDTO `json:"memory_citation"`
}

type statementDTO struct {
	Text      string `json:"text"`
	Citations []any  `json:"citations"`
}

type omissionDTO struct {
	Text     string `json:"text"`
	Citation any    `json:"citation"`
}

type contentDTO struct {
	Schema      string         `json:"schema"`
	Objective   string         `json:"objective"`
	State       []statementDTO `json:"state"`
	Disposition string         `json:"disposition"`
	NextAction  *statementDTO  `json:"next_action"`
	Omissions   []omissionDTO  `json:"omissions"`
}

func RenderContent(content Content) ([]byte, error) {
	if err := content.Validate(); err != nil {
		return nil, err
	}
	return marshalIndented(contentWire(content))
}

func Render(value any, audience Audience) (string, error) {
	if audience != Human && audience != Agent {
		return "", fmt.Errorf("unsupported Handoff audience: %s", audience)
	}
	var content Content
	switch typed := value.(type) {
	case Draft:
		content = typed.AsContent()
	case Prepared:
		content = typed.Content()
	case artifact.Artifact[Content]:
		content = typed.Content()
	default:
		return "", fmt.Errorf("unsupported Handoff render value %T", value)
	}
	encoded, err := RenderContent(content)
	return string(encoded), err
}

func contentWire(content Content) contentDTO {
	state := make([]statementDTO, len(content.state))
	for index, statement := range content.state {
		state[index] = statementWire(statement)
	}
	var next *statementDTO
	if content.nextAction != nil {
		value := statementWire(*content.nextAction)
		next = &value
	}
	omissions := make([]omissionDTO, len(content.omissions))
	for index, omission := range content.omissions {
		omissions[index] = omissionDTO{Text: omission.text, Citation: citationWire(omission.citation)}
	}
	return contentDTO{
		Schema: content.Schema(), Objective: content.objective, State: state,
		Disposition: string(content.disposition), NextAction: next, Omissions: omissions,
	}
}

func statementWire(statement Statement) statementDTO {
	citations := make([]any, len(statement.citations))
	for index, citation := range statement.citations {
		citations[index] = citationWire(citation)
	}
	return statementDTO{Text: statement.text, Citations: citations}
}

func citationWire(citation Citation) any {
	switch value := citation.(type) {
	case nil:
		return nil
	case SourceCitation:
		return sourceCitationDTO{Kind: string(value.Kind()), SourceRef: sourceRefWire(value.ref)}
	case ArtifactCitation:
		return artifactCitationDTO{Kind: string(value.Kind()), ArtifactRef: artifactRefWire(value.ref)}
	case MemoryCitation:
		memoryValue := value.citation
		return handoffMemoryCitationDTO{
			Kind: string(value.Kind()),
			MemoryCitation: memoryCitationDTO{
				MemoryRef: artifactRefWire(memoryValue.MemoryRef),
				EntryID:   memoryValue.EntryID, EntryVersionID: memoryValue.EntryVersionID,
			},
		}
	default:
		return nil
	}
}

func sourceRefWire(ref source.Ref) sourceRefDTO {
	return sourceRefDTO{SourceType: ref.Type(), SourceID: ref.ID()}
}

func artifactRefWire(ref artifact.Ref) artifactRefDTO {
	return artifactRefDTO{Family: ref.Family(), ArtifactID: ref.ID(), Revision: ref.Revision()}
}

func marshalIndented(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return slices.Clone(bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})), nil
}
