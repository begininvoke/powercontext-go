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

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func generateSBOM(syft, root, output, syftVersion string, created time.Time) error {
	sourceName := filepath.Base(root)
	command := exec.Command(
		syft,
		"scan", "dir:"+root,
		"--source-name", sourceName,
		"--output", "spdx-json="+output,
	)
	command.Env = append(os.Environ(), "SYFT_CHECK_FOR_APP_UPDATE=false")
	combined, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("generate SPDX SBOM: %w: %s", err, strings.TrimSpace(string(combined)))
	}
	contents, err := readBoundedFile(output, maxMetadataBytes)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(contents, &document); err != nil || document["spdxVersion"] == nil {
		return errors.New("Syft did not produce a valid SPDX JSON document")
	}
	creationInfo, ok := document["creationInfo"].(map[string]any)
	if !ok {
		return errors.New("Syft SPDX document has no creationInfo object")
	}
	document["name"] = sourceName
	document["documentNamespace"] = "https://github.com/ob-labs/powercontext-go/sbom/" + sourceName
	creationInfo["created"] = created.UTC().Format(time.RFC3339)
	creationInfo["creators"] = []string{
		"Organization: Anchore, Inc",
		"Tool: syft-" + syftVersion,
	}
	return writeJSON(output, document)
}
