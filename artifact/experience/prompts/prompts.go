// Package prompts owns the frozen Experience inference instructions.
package prompts

import (
	_ "embed"
	"slices"
	"strings"
)

const (
	IncubationVersion = "powercontext.experience.incubate.v1"
	GenerationVersion = "powercontext.experience.generate.v1"
)

//go:embed incubation.txt
var incubation string

//go:embed generation.txt
var generation string

//go:embed incubation.schema.json
var incubationSchema []byte

//go:embed generation.schema.json
var generationSchema []byte

func Incubation() string       { return strings.TrimSuffix(incubation, "\n") }
func Generation() string       { return strings.TrimSuffix(generation, "\n") }
func IncubationSchema() []byte { return slices.Clone(incubationSchema) }
func GenerationSchema() []byte { return slices.Clone(generationSchema) }
