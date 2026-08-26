package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnvironmentFileUsesStrictDotenvSubsetAndOverridesProcess(t *testing.T) {
	t.Setenv("POWERCONTEXT_LOCOMO_TEST_DOUBLE", "before")
	t.Setenv("POWERCONTEXT_LOCOMO_TEST_SINGLE", "before")
	t.Setenv("POWERCONTEXT_LOCOMO_TEST_BARE", "before")
	path := filepath.Join(t.TempDir(), ".env")
	contents := strings.Join([]string{
		"# benchmark configuration",
		`export POWERCONTEXT_LOCOMO_TEST_DOUBLE="line\nvalue"`,
		"POWERCONTEXT_LOCOMO_TEST_SINGLE='literal # value'",
		"POWERCONTEXT_LOCOMO_TEST_BARE=plain value # comment",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvironmentFile(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("POWERCONTEXT_LOCOMO_TEST_DOUBLE") != "line\nvalue" ||
		os.Getenv("POWERCONTEXT_LOCOMO_TEST_SINGLE") != "literal # value" ||
		os.Getenv("POWERCONTEXT_LOCOMO_TEST_BARE") != "plain value" {
		t.Fatal("dotenv values were not decoded or did not override the process environment")
	}

	invalid := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(invalid, []byte("NOT-AN-ENV=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvironmentFile(invalid); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("invalid environment assignment error = %v", err)
	}
}

func TestRunFlagValidationMatchesPythonPositiveTypes(t *testing.T) {
	for _, arguments := range [][]string{
		{"--answer-k", "0"}, {"--conversation-limit", "0"}, {"--question-limit", "0"},
	} {
		if _, err := parseRunFlags(arguments); err == nil {
			t.Fatalf("flags %v accepted an explicit zero", arguments)
		}
	}
	flags, err := parseRunFlags([]string{"--top-k", "30"})
	if err != nil || flags.answerK != 0 || flags.conversationLimit != 0 {
		t.Fatalf("omitted optional flags = %+v, %v", flags, err)
	}
	categories, err := parseCategories("1, 2,2, 4,")
	if err != nil || !reflect.DeepEqual(categories, []int{1, 2, 4}) {
		t.Fatalf("categories = %v, %v", categories, err)
	}
}
