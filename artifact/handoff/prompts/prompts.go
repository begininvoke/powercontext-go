// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
