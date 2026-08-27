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

package oceanbase

import (
	"strings"
	"testing"
)

func TestMemoryVectorDDLUsesBinaryIdentityCollation(t *testing.T) {
	statement := memoryVectorDDL(384)
	if !strings.Contains(statement, "embedding VECTOR(384) NOT NULL") {
		t.Fatalf("vector DDL has wrong dimension:\n%s", statement)
	}
	for _, line := range strings.Split(statement, "\n") {
		if strings.Contains(line, "VARCHAR(") &&
			!strings.Contains(line, " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin") {
			t.Errorf("identity column lacks binary collation: %s", strings.TrimSpace(line))
		}
	}
}
