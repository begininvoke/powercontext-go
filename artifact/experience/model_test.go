package experience_test

import (
	"strings"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact/experience"
)

func TestExperienceContentPreservesCompleteReusableJudgment(t *testing.T) {
	content, err := experience.NewContent(
		"The public OpenAPI contract changed.",
		"Regenerate the checked-in Client and run contract tests.",
		"The generated transport remained aligned with the server.",
		"Regenerate the Client before validating public contract changes.",
	)
	if err != nil {
		t.Fatal(err)
	}
	if content.Situation() != "The public OpenAPI contract changed." ||
		content.Action() != "Regenerate the checked-in Client and run contract tests." ||
		content.Outcome() != "The generated transport remained aligned with the server." ||
		content.Lesson() != "Regenerate the Client before validating public contract changes." {
		t.Fatalf("content = %#v", content)
	}
}

func TestExperienceContentRequiresEveryJudgmentPart(t *testing.T) {
	valid := [4]string{"situation", "action", "outcome", "lesson"}
	for missing := range valid {
		values := valid
		values[missing] = ""
		if _, err := experience.NewContent(values[0], values[1], values[2], values[3]); err == nil {
			t.Fatalf("missing field %d was accepted", missing)
		}
	}
}

func TestExperienceContentRejectsBlankJudgmentParts(t *testing.T) {
	valid := [4]string{"situation", "action", "outcome", "lesson"}
	for field := range valid {
		for _, blank := range []string{"\n\t", "\u001c\u001f"} {
			values := valid
			values[field] = blank
			if _, err := experience.NewContent(values[0], values[1], values[2], values[3]); err == nil {
				t.Fatalf("blank field %d value %q was accepted", field, blank)
			}
		}
	}
}

func TestExperienceContentBoundsEveryJudgmentPart(t *testing.T) {
	valid := [4]string{"situation", "action", "outcome", "lesson"}
	for field := range valid {
		values := valid
		values[field] = strings.Repeat("界", experience.MaxFieldLength+1)
		if _, err := experience.NewContent(values[0], values[1], values[2], values[3]); err == nil {
			t.Fatalf("overlong field %d was accepted", field)
		}
	}
}
