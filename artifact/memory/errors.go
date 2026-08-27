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

package memory

import "fmt"

type InvalidOperationError struct{ Code string }

func (e *InvalidOperationError) Error() string {
	messages := map[string]string{
		"append-entries":   "memory append mode requires at least one entry",
		"extract-evidence": "memory extract mode requires evidence and does not accept entries",
		"no-work":          "memory remember call has no executable work",
		"duplicate-target": "a memory entry can only be targeted once per operation",
		"id-collision":     "generated memory identity collides with current content",
		"organize-mode":    "unsupported memory organize mode",
		"since-greater":    "since_revision cannot be greater than the target Revision",
		"since-negative":   "since_revision cannot be negative",
		"search-memories":  "memory search requires at least one explicit Memory ref",
		"search-limit":     "memory search limit must be positive",
		"search-mode":      "unsupported memory search mode",
	}
	if message, ok := messages[e.Code]; ok {
		return message
	}
	return "invalid Memory operation: " + e.Code
}

type InvalidCandidateError struct {
	Code   string
	Detail string
}

func (e *InvalidCandidateError) Error() string {
	if e.Detail == "" {
		return "invalid Memory candidate: " + e.Code
	}
	return fmt.Sprintf("invalid Memory candidate %s: %s", e.Code, e.Detail)
}

type InvalidEvidenceError struct{ Code string }

func (e *InvalidEvidenceError) Error() string {
	messages := map[string]string{
		"source-resolver":   "Memory Source evidence requires a configured resolver",
		"artifact-resolver": "Memory Artifact evidence requires a configured resolver",
		"source-adapter":    "Memory Source evidence has no registered adapter",
		"source-outside":    "Memory candidate cites Source evidence outside the operation",
		"artifact-outside":  "Memory candidate cites Artifact evidence outside the operation",
	}
	if message, ok := messages[e.Code]; ok {
		return message
	}
	return "invalid Memory evidence: " + e.Code
}

type EntryNotFoundError struct{ EntryID string }

func (e *EntryNotFoundError) Error() string { return "Memory entry was not found: " + e.EntryID }

type EntryInactiveError struct{ EntryID string }

func (e *EntryInactiveError) Error() string { return "Memory entry is inactive: " + e.EntryID }

type InvalidEmbeddingError struct{ Code string }

func (e *InvalidEmbeddingError) Error() string {
	if e.Code == "count" {
		return "embedding result count does not match Memory input count"
	}
	return "invalid Memory embedding: " + e.Code
}

type CapabilityNotSupportedError struct {
	Capability string
	Detail     string
}

func (e *CapabilityNotSupportedError) Error() string {
	message := "memory capability is not supported: " + e.Capability
	if e.Detail != "" {
		message += " (" + e.Detail + ")"
	}
	return message
}

type InvalidCitationError struct{ Code string }

func (e *InvalidCitationError) Error() string {
	messages := map[string]string{
		"base-mismatch":      "memory value does not match its exact stored Revision",
		"memory-mismatch":    "memory value does not match its exact stored Revision",
		"duplicate-versions": "backend returned duplicate entry version identities",
		"missing-version":    "manifest entry version is missing",
		"cross-identity":     "entry version crosses Memory or logical entry identity",
		"hash-mismatch":      "manifest content hash does not match canonical entry bytes",
		"projection-version": "memory active manifest and projections differ",
		"expand-count":       "invalid memory citation expansion count",
		"expand-anchor":      "invalid memory citation anchor",
		"entry-missing":      "memory revision candidate is missing its target entry",
		"entry-mismatch":     "memory entry does not match its exact current version",
	}
	if message, ok := messages[e.Code]; ok {
		return message
	}
	return "invalid memory citation: " + e.Code
}

type BackendConfigurationError struct{ Detail string }

func (e *BackendConfigurationError) Error() string { return e.Detail }

// CanonicalError reports a stable Memory canonicalization failure.
type CanonicalError struct {
	Code   string
	Detail any
}

func (e *CanonicalError) Error() string {
	switch e.Code {
	case "text-too-long":
		return "memory entry text must not exceed 8192 UTF-8 bytes"
	case "reason-too-long":
		return "memory change reason must not exceed 512 Unicode code points"
	case "identifier-empty":
		return "memory identifiers must be non-empty strings"
	case "identifier-ascii":
		return "memory identifiers must contain only ASCII characters"
	case "identifier-too-long":
		return "memory identifiers must not exceed 128 ASCII characters"
	case "hash":
		return "content hashes must be 64 lowercase hexadecimal characters"
	case "dimension-positive":
		return "embedding dimension must be positive"
	case "vector-dimension":
		return fmt.Sprintf("embedding must contain exactly %v dimensions", e.Detail)
	case "vector-finite":
		return "embedding values must all be finite"
	case "vector-zero":
		return "embedding must have a non-zero norm"
	case "string-empty":
		return fmt.Sprintf("%v must be non-empty", e.Detail)
	default:
		return "invalid canonical Memory value"
	}
}
