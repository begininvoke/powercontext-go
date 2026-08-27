package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/internal/contextpack"
	serverlogging "github.com/ob-labs/powercontext-go/internal/observability/logging"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
	"github.com/ob-labs/powercontext-go/internal/stats"
)

type relationalModelUsageRecorder struct {
	database   *sqlstore.Database
	repository sqlstore.StatisticsRepository
	clock      func() time.Time
	logger     *slog.Logger
}

func (r relationalModelUsageRecorder) RecordModelUsage(
	ctx context.Context,
	scopeID string,
	purpose stats.ModelPurpose,
	operation stats.ModelOperation,
	usage inference.Usage,
) {
	err := r.database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		return r.repository.Record(ctx, tx, scopeID, r.clock().UTC(), purpose, operation, usage)
	})
	if err == nil {
		return
	}
	serverlogging.LogSafely(
		ctx,
		namedLogger(r.logger, "powercontext.builtin.runtime.application"),
		slog.LevelError,
		"Model usage recording failed",
		slog.String("event", "statistics.model_usage.failed"),
		slog.String("purpose", string(purpose)),
		slog.String("operation", string(operation)),
		slog.String("outcome", "failure"),
		slog.String("unit", "statistics"),
	)
}

type relationalRecallStatistics struct {
	database   *sqlstore.Database
	sources    *sqlstore.SourceRepository
	artifacts  *sqlstore.ArtifactRepository
	repository sqlstore.StatisticsRepository
	estimator  *inference.TokenEstimator
	clock      func() time.Time
	logger     *slog.Logger
}

func (r relationalRecallStatistics) ObservePreparedContext(
	ctx context.Context,
	scopeID string,
	build contextpack.Build,
) {
	estimator, err := sqlstore.NewRelationalRecallTokenEstimator(
		r.database, scopeID, r.sources, r.artifacts, r.estimator,
	)
	if err == nil {
		var measurement stats.RecallTokenMeasurement
		measurement, err = estimator.Estimate(ctx, build)
		if err == nil {
			err = r.database.Transaction(ctx, func(tx sqlstore.DBTX) error {
				return r.repository.RecordRecall(ctx, tx, scopeID, r.clock().UTC(), measurement)
			})
			if err != nil {
				r.logFailure(ctx, "statistics.recall_tokens.failed", "Recall token recording failed")
			}
			return
		}
	}
	r.logFailure(ctx, "statistics.recall_tokens.estimation_failed", "Recall token estimation failed")
}

func (r relationalRecallStatistics) logFailure(ctx context.Context, event, message string) {
	serverlogging.LogSafely(
		ctx,
		namedLogger(r.logger, "powercontext.builtin.runtime.application"),
		slog.LevelError,
		message,
		slog.String("event", event),
		slog.String("outcome", "failure"),
		slog.String("unit", "statistics"),
	)
}
