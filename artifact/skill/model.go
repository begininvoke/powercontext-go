package skill

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/thunguo/powercontext-go/artifact"
	"github.com/thunguo/powercontext-go/source"
)

const (
	Family                  = "skill"
	MaxNameLength           = 128
	MaxDescriptionLength    = 2_000
	MaxInstructionsLength   = 32_000
	MaxValidationItems      = 32
	MaxValidationItemLength = 2_000
)

type Content struct {
	name         string
	description  string
	instructions string
	validation   []string
}

func NewContent(name, description, instructions string, validation []string) (Content, error) {
	if err := trimmedBounded("Skill name", name, MaxNameLength); err != nil {
		return Content{}, err
	}
	if err := trimmedBounded("Skill description", description, MaxDescriptionLength); err != nil {
		return Content{}, err
	}
	if strings.TrimSpace(instructions) == "" || utf8.RuneCountInString(instructions) > MaxInstructionsLength {
		return Content{}, fmt.Errorf("Skill instructions must be non-blank and not exceed %d characters", MaxInstructionsLength)
	}
	if len(validation) < 1 || len(validation) > MaxValidationItems {
		return Content{}, fmt.Errorf("Skill validation must contain 1..%d items", MaxValidationItems)
	}
	for _, item := range validation {
		if err := trimmedBounded("Skill validation item", item, MaxValidationItemLength); err != nil {
			return Content{}, err
		}
	}
	return Content{name: name, description: description, instructions: instructions, validation: slices.Clone(validation)}, nil
}

func (c Content) Name() string         { return c.name }
func (c Content) Description() string  { return c.description }
func (c Content) Instructions() string { return c.instructions }
func (c Content) Validation() []string { return slices.Clone(c.validation) }

type Skill = artifact.Artifact[Content]
type Draft = artifact.Draft[Content]

func NewDraft(content Content, sources []source.Ref, artifacts []artifact.Ref) (Draft, error) {
	return artifact.NewDraft(Family, content, sources, artifacts)
}

func trimmedBounded(label, value string, maximum int) error {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must be non-empty and trimmed", label)
	}
	if utf8.RuneCountInString(value) > maximum {
		return fmt.Errorf("%s must not exceed %d characters", label, maximum)
	}
	return nil
}
