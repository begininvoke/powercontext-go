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

// Package prompts owns the frozen Memory inference instructions.
package prompts

import (
	_ "embed"
	"slices"
	"strings"
)

const (
	CodingVersion       = "powercontext.memory.extract.v1"
	ConversationVersion = "powercontext.memory.extract.conversation.v1"
	RerankVersion       = "powercontext.memory.rerank.listwise.v1"
)

// The source prompt literals are Python .strip() values. Text files retain a
// conventional terminal LF, which is removed exactly once at the embed edge.

//go:embed coding.txt
var coding string

//go:embed conversation.txt
var conversation string

//go:embed rerank.txt
var rerank string

//go:embed extraction.schema.json
var extractionSchema []byte

//go:embed rerank.schema.json
var rerankSchema []byte

func Coding() string           { return trimTerminalLF(coding) }
func Conversation() string     { return trimTerminalLF(conversation) }
func Rerank() string           { return trimTerminalLF(rerank) }
func ExtractionSchema() []byte { return slices.Clone(extractionSchema) }
func RerankSchema() []byte     { return slices.Clone(rerankSchema) }

func trimTerminalLF(value string) string { return strings.TrimSuffix(value, "\n") }
