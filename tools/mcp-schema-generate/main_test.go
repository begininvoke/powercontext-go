package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ghodss/yaml"
)

func TestGeneratedSchemasMatchOpenAPI(t *testing.T) {
	t.Parallel()
	output := filepath.Join(t.TempDir(), "schemas_gen.go")
	if err := run("../../openapi/powercontext.yaml", output); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../internal/mcpapi/schemas_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("MCP schemas are stale; run go generate ./internal/mcpapi")
	}
}

func TestCollectedSchemasAreObjectSchemas(t *testing.T) {
	t.Parallel()
	spec, err := os.ReadFile("../../openapi/powercontext.yaml")
	if err != nil {
		t.Fatal(err)
	}
	jsonSpec, err := yamlToJSON(spec)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(jsonSpec, &root); err != nil {
		t.Fatal(err)
	}
	definitions, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 23 {
		t.Fatalf("definitions = %d, want 23", len(definitions))
	}
	reportCount := 0
	for _, definition := range definitions {
		for label, raw := range map[string][]byte{"input": definition.input, "output": definition.output} {
			var schema map[string]any
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("%s %s schema: %v", definition.name, label, err)
			}
			if schema["type"] != "object" {
				t.Fatalf("%s %s type = %#v, want object", definition.name, label, schema["type"])
			}
		}
		if definition.report {
			reportCount++
		}
	}
	if reportCount != 3 {
		t.Fatalf("Handoff Report schemas = %d, want 3", reportCount)
	}
}

func yamlToJSON(value []byte) ([]byte, error) {
	return yaml.YAMLToJSON(value)
}
