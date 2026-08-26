// Package prompts owns the frozen Handoff inference instructions.
package prompts

import (
	_ "embed"
	"slices"
	"strings"
)

const GenerationVersion = "powercontext.handoff.generate.v1"

//go:embed generation.txt
var generation string

//go:embed generation.schema.json
var generationSchema []byte

func Generation() string       { return strings.TrimSuffix(generation, "\n") }
func GenerationSchema() []byte { return slices.Clone(generationSchema) }
