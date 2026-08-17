package experience

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/thunguo/powercontext-go/artifact"
	"github.com/thunguo/powercontext-go/artifact/memory"
	"github.com/thunguo/powercontext-go/source"
)

const (
	Family         = "experience"
	MaxFieldLength = 8_000
)

type Content struct {
	situation string
	action    string
	outcome   string
	lesson    string
}

func NewContent(situation, action, outcome, lesson string) (Content, error) {
	for field, value := range map[string]string{
		"situation": situation, "action": action, "outcome": outcome, "lesson": lesson,
	} {
		if strings.TrimSpace(value) == "" {
			return Content{}, fmt.Errorf("Experience fields must not be blank: %s", field)
		}
		if utf8.RuneCountInString(value) > MaxFieldLength {
			return Content{}, fmt.Errorf("Experience field %s must not exceed %d characters", field, MaxFieldLength)
		}
	}
	return Content{situation: situation, action: action, outcome: outcome, lesson: lesson}, nil
}

func (c Content) Situation() string { return c.situation }
func (c Content) Action() string    { return c.action }
func (c Content) Outcome() string   { return c.outcome }
func (c Content) Lesson() string    { return c.lesson }

type Experience = artifact.Artifact[Content]
type Draft = artifact.Draft[Content]

func NewDraft(content Content, sources []source.Ref, artifacts []artifact.Ref) (Draft, error) {
	return artifact.NewDraft(Family, content, sources, artifacts)
}

type SearchHit struct {
	ArtifactRef artifact.Ref
	Content     Content
}

func Render(content Content) string {
	return "Situation: " + content.situation + "\n" +
		"Action: " + content.action + "\n" +
		"Outcome: " + content.outcome + "\n" +
		"Lesson: " + content.lesson
}

func SearchText(content Content) string {
	return strings.Join([]string{content.situation, content.action, content.outcome, content.lesson}, "\n")
}

func SearchableText(content Content) string { return memory.AnalyzeText(SearchText(content)) }
