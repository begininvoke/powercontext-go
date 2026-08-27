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

package skill

import (
	"strings"
	"testing"
)

func TestFrontmatterScalarRejectsEmptyOrMalformedQuotedValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty plain", value: "", want: "must not be empty"},
		{name: "empty double quoted", value: `""`, want: "must not be empty"},
		{name: "empty single quoted", value: `''`, want: "must not be empty"},
		{name: "unterminated double quoted", value: `"unterminated`, want: "invalid scalar"},
		{name: "unterminated single quoted", value: `'unterminated`, want: "invalid scalar"},
		{name: "lone single quote", value: `'`, want: "invalid scalar"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := frontmatterScalar(test.value)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("frontmatterScalar(%q) error = %v, want %q", test.value, err, test.want)
			}
		})
	}
}

func TestFrontmatterScalarParsesSupportedStrings(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: `"Codex Skill"`, want: "Codex Skill"},
		{value: `'Claude Code Skill'`, want: "Claude Code Skill"},
		{value: "unquoted", want: "unquoted"},
	}
	for _, test := range tests {
		got, err := frontmatterScalar(test.value)
		if err != nil || got != test.want {
			t.Fatalf("frontmatterScalar(%q) = %q, %v; want %q", test.value, got, err, test.want)
		}
	}
}
