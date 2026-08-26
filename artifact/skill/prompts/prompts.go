// Package prompts owns the frozen managed-Skill inference instructions.
package prompts

import (
	_ "embed"
	"slices"
	"strings"
)

const GenerationVersion = "powercontext.skill.generate.v2"

//go:embed generation.txt
var generation string

//go:embed generation.schema.json
var generationSchema []byte

func Generation() string       { return strings.TrimSuffix(generation, "\n") }
func GenerationSchema() []byte { return slices.Clone(generationSchema) }
