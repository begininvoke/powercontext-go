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

package trigger

import (
	"fmt"

	"github.com/ob-labs/powercontext-go/source"
)

const SourceWindowName = "memory-source-window"

type SourceHighWatermark struct {
	sequence int64
	limit    int64
}

func NewSourceHighWatermark(sequence, limit int64) (SourceHighWatermark, error) {
	if sequence < 0 {
		return SourceHighWatermark{}, fmt.Errorf("Source high watermark sequence must be non-negative")
	}
	if limit < 1 {
		return SourceHighWatermark{}, fmt.Errorf("Source window limit must be positive")
	}
	return SourceHighWatermark{sequence: sequence, limit: limit}, nil
}

func (s SourceHighWatermark) Sequence() int64 { return s.sequence }
func (s SourceHighWatermark) Limit() int64    { return s.limit }

type ProcessSourceWindow struct {
	after   int64
	through int64
}

func (w ProcessSourceWindow) After() int64   { return w.after }
func (w ProcessSourceWindow) Through() int64 { return w.through }

type SourceWindowPolicy struct{}

func (SourceWindowPolicy) InitialState() source.Cursor { return source.NewCursor(0) }
func (SourceWindowPolicy) Activate(
	signal SourceHighWatermark,
	state source.Cursor,
) Transition[source.Cursor, ProcessSourceWindow] {
	if signal.sequence <= state.Sequence() {
		return NewTransition[source.Cursor, ProcessSourceWindow](state)
	}
	through := min(signal.sequence, state.Sequence()+signal.limit)
	return NewTransition(
		source.NewCursor(through),
		ProcessSourceWindow{after: state.Sequence(), through: through},
	)
}
