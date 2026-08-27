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

package scheduler

import (
	"encoding/base64"
	"errors"
	"testing"
)

func FuzzRestrictedPickleJobDecoder(f *testing.F) {
	valid, err := base64.StdEncoding.DecodeString(pythonSourceWindowPickle)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte{0x80, 5, 'N', '.'})
	f.Add([]byte{0x80, 5, 0x8c, 5, 'p', 'o', 's', 'i', 'x', 0x8c, 6, 's', 'y', 's', 't', 'e', 'm', 0x93, ')', 'R', '.'})
	f.Fuzz(func(t *testing.T, blob []byte) {
		_, err := decodeJobState(
			blob,
			SourceWindowJobID,
			"/private/tmp/powercontext-scheduler-oracle.db",
			1786907189.018739,
		)
		if err == nil {
			return
		}
		var invalid *InvalidJobStateError
		if !errors.As(err, &invalid) {
			t.Fatalf("decoder returned unclassified error %T: %v", err, err)
		}
	})
}
