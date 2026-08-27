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

import (
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
)

func TestMemoryModelsExpressExactRevisionAndEntryIdentity(t *testing.T) {
	entry, err := NewManifestEntry("entry-a", "version-a1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Active)
	if err != nil {
		t.Fatal(err)
	}
	versionID := "version-a1"
	change, err := NewChange(Add, "entry-a", nil, &versionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := NewContent(NewManifest([]ManifestEntry{entry}), []Change{change})
	draft, err := NewDraft(content, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := artifact.New("memory-a", 1, draft)
	if err != nil {
		t.Fatal(err)
	}
	wantRef, err := artifact.NewRef(Family, "memory-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if value.Family() != Family || value.Ref() != wantRef || value.Content().Schema() != "powercontext.memory.v1" ||
		value.Content().Manifest().Format() != "flat-v1" {
		t.Fatalf("Memory identity contract = %#v", value)
	}
}

func TestMemoryEntryKindRemainsOpenAndNonEmptyAfterCanonicalization(t *testing.T) {
	input := NewEntryInput(nil, "integration-owned-kind", "Durable text.", nil, nil, nil)
	if input.Kind() != "integration-owned-kind" || len(input.Sources()) != 0 || len(input.Artifacts()) != 0 {
		t.Fatalf("entry input = %#v", input)
	}
	if normalized, err := NormalizeKind(input.Kind()); err != nil || normalized != input.Kind() {
		t.Fatalf("open kind normalization = %q, %v", normalized, err)
	}
	if _, err := NormalizeKind("   "); err == nil {
		t.Fatal("empty Memory kind was accepted")
	}
}
