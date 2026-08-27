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
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

func skillMetadata(manifest, packageName string, agentKind AgentKind) (string, string, error) {
	info, err := os.Lstat(manifest)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("Agent Skill manifest must be a regular non-symlink file")
	}
	contents, err := readBounded(manifest, MaxExternalManifestBytes)
	if err != nil {
		return "", "", err
	}
	return parseSkillMetadata(contents, packageName, agentKind)
}

func parseSkillMetadata(contents []byte, packageName string, agentKind AgentKind) (string, string, error) {
	if !utf8.Valid(contents) {
		return "", "", fmt.Errorf("Agent Skill manifest must be UTF-8")
	}
	if !utf8.ValidString(packageName) {
		return "", "", fmt.Errorf("Agent Skill package name must be UTF-8")
	}
	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	scanner.Buffer(make([]byte, 0, 4*1024), MaxExternalManifestBytes+1)
	if !scanner.Scan() || trimPythonWhitespace(scanner.Text()) != "---" {
		return "", "", fmt.Errorf("Agent Skill manifest is missing frontmatter")
	}
	metadata := make(map[string]string)
	terminated := false
	for scanner.Scan() {
		line := scanner.Text()
		if trimPythonWhitespace(line) == "---" {
			terminated = true
			break
		}
		field, raw, found := strings.Cut(line, ":")
		if found && (field == "name" || field == "description") {
			value, err := frontmatterScalar(trimPythonWhitespace(raw))
			if err != nil {
				return "", "", err
			}
			metadata[field] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}
	if !terminated {
		return "", "", fmt.Errorf("Agent Skill frontmatter is not terminated")
	}
	name, hasName := metadata["name"]
	description, hasDescription := metadata["description"]
	if agentKind == ClaudeCodeAgent && !hasName {
		name, hasName = packageName, true
	}
	if !hasName || !hasDescription {
		required := "name and description"
		if agentKind == ClaudeCodeAgent {
			required = "description"
		}
		return "", "", fmt.Errorf("%s Skill frontmatter requires %s", agentKind, required)
	}
	return name, description, nil
}

func frontmatterScalar(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("Agent Skill frontmatter values must not be empty")
	}
	if value[0] == '"' {
		var parsed string
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return "", fmt.Errorf("Agent Skill frontmatter contains an invalid scalar")
		}
		return parsed, nil
	}
	if value[0] == '\'' {
		runes := []rune(value)
		if len(runes) == 1 {
			return "", nil
		}
		return string(runes[1 : len(runes)-1]), nil
	}
	return value, nil
}
