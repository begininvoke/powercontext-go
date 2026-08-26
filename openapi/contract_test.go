package openapi

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
)

const frozenOpenAPISHA256 = "739e8fa4983399980d6c619490fe1806dc354ac173a5bb3c99250cb080d11b09"

func TestFrozenOpenAPIAndGeneratedHandlerStayInSync(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile("powercontext.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(contents)); got != frozenOpenAPISHA256 {
		t.Fatalf("OpenAPI SHA-256 = %s, want frozen Oracle %s", got, frozenOpenAPISHA256)
	}

	operationIDs := make(map[string]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "operationId:") {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(line, "operationId:"))
		if id == "" {
			t.Fatal("OpenAPI contains a blank operationId")
		}
		if _, duplicate := operationIDs[id]; duplicate {
			t.Fatalf("duplicate operationId %q", id)
		}
		operationIDs[id] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if got := len(operationIDs); got != 53 {
		t.Fatalf("OpenAPI operations = %d, want 53", got)
	}
	handler := reflect.TypeOf((*v1.Handler)(nil)).Elem()
	if got := handler.NumMethod(); got != len(operationIDs) {
		t.Fatalf("generated Handler methods = %d, OpenAPI operations = %d", got, len(operationIDs))
	}
}
