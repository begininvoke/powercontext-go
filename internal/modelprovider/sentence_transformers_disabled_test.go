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

//go:build !local_embeddings || !cgo || (!ORT && !ALL)

package modelprovider

import (
	"errors"
	"strings"
	"testing"

	"github.com/ob-labs/powercontext-go/inference"
)

func TestSentenceTransformersRequiresFullBuild(t *testing.T) {
	factory, err := NewFactory(MilestoneB, testEnvironment{}.lookup, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = factory.EmbeddingTransport("sentence-transformers:sentence-transformers/all-MiniLM-L6-v2")
	var configuration *inference.ConfigurationError
	if !errors.As(err, &configuration) || configuration.Code() != "embedding-model" {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "local_embeddings") || strings.Contains(err.Error(), "ORT") {
		t.Fatalf("public error exposed build details: %v", err)
	}
}
