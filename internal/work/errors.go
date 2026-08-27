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

package work

import "fmt"

// InvalidError identifies a domain validation failure without exposing the
// submitted value. Transport adapters map it to the public invalid_request
// envelope.
type InvalidError struct {
	Field  string
	Detail string
}

func (e *InvalidError) Error() string {
	if e.Detail == "" {
		return "invalid Work " + e.Field
	}
	return fmt.Sprintf("invalid Work %s: %s", e.Field, e.Detail)
}

// InvalidRequestError identifies a validly decoded request whose selected
// history cannot satisfy the Work continuity trust boundary.
type InvalidRequestError struct{ Code string }

func (e *InvalidRequestError) Error() string { return "invalid Work request: " + e.Code }
