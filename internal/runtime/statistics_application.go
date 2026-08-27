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

package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/ob-labs/powercontext-go/internal/stats"
)

// StatisticsReader is the exact read surface needed by the product-facing
// application. Persistence implementations may additionally record usage,
// but the HTTP operation does not depend on those mutation methods.
type StatisticsReader interface {
	Overview(context.Context, stats.Period, time.Time) (stats.Statistics, error)
}

type StatisticsReaderFactory func(string) (StatisticsReader, error)

type StatisticsApplication struct {
	runtime *Runtime
	readers StatisticsReaderFactory
	clock   Clock
}

func NewStatisticsApplication(
	runtime *Runtime,
	readers StatisticsReaderFactory,
	clock Clock,
) (*StatisticsApplication, error) {
	if runtime == nil || readers == nil {
		return nil, errors.New("runtime: Statistics application dependencies must not be nil")
	}
	if clock == nil {
		clock = time.Now
	}
	return &StatisticsApplication{runtime: runtime, readers: readers, clock: clock}, nil
}

func (a *StatisticsApplication) Overview(
	ctx context.Context,
	scopeID string,
	period stats.Period,
) (stats.Statistics, error) {
	var result stats.Statistics
	err := a.runtime.ScopedRead(ctx, scopeID, func(ctx context.Context, scope string) error {
		reader, err := a.readers(scope)
		if err != nil {
			return err
		}
		if reader == nil {
			return &StateError{Code: "statistics"}
		}
		result, err = reader.Overview(ctx, period, a.clock().UTC())
		return err
	})
	return result, err
}
