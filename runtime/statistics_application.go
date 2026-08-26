package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/ob-labs/powercontext-go/stats"
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
