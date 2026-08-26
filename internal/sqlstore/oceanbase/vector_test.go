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
