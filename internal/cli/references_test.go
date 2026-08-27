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

package cli

import (
	"strings"
	"testing"
)

func TestArtifactReferenceSplitsRevisionAtLastAtSign(t *testing.T) {
	t.Parallel()

	reference, err := artifactReference("experience/user@example.com@12")
	if err != nil {
		t.Fatalf("artifactReference() error = %v", err)
	}
	if reference.Family != "experience" || reference.ArtifactID != "user@example.com" || reference.Revision != 12 {
		t.Fatalf("artifactReference() = %#v", reference)
	}
}

func TestReferenceListsRejectDuplicates(t *testing.T) {
	t.Parallel()

	if _, err := sourceReferences([]string{"content/task-1", "content/task-1"}); err == nil ||
		!strings.Contains(err.Error(), "duplicate Source reference") {
		t.Fatalf("sourceReferences() error = %v", err)
	}
	if _, err := artifactReferences([]string{"skill/tool@1", "skill/tool@1"}); err == nil ||
		!strings.Contains(err.Error(), "duplicate Artifact reference") {
		t.Fatalf("artifactReferences() error = %v", err)
	}
}

func TestArtifactReferenceRejectsMalformedValue(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"experience/id", "experience/@1", "/id@1", "experience/id@0", " experience/id@1"} {
		if _, err := artifactReference(value); err == nil {
			t.Errorf("artifactReference(%q) unexpectedly succeeded", value)
		}
	}
}
