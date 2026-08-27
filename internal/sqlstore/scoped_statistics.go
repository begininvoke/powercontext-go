package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/internal/stats"
	"github.com/ob-labs/powercontext-go/trigger"
)

// ScopedStatistics assembles one scope's inventory and bounded daily usage in
// a single relational snapshot.
type ScopedStatistics struct {
	database         *Database
	scopeID          string
	memoryArtifactID string
	artifacts        *ArtifactRepository
	cursors          SourceCursorRepository
	repository       StatisticsRepository
	estimator        *inference.TokenEstimatorProfile
}

func NewScopedStatistics(
	database *Database,
	scopeID, memoryArtifactID string,
	artifacts *ArtifactRepository,
	repository StatisticsRepository,
	estimator *inference.TokenEstimatorProfile,
) (*ScopedStatistics, error) {
	if database == nil || artifacts == nil {
		return nil, errors.New("sqlstore: statistics database and Artifact repository must not be nil")
	}
	if err := requireScope(scopeID); err != nil {
		return nil, err
	}
	if _, err := artifact.NewRef(memory.Family, memoryArtifactID, 1); err != nil {
		return nil, err
	}
	var profile *inference.TokenEstimatorProfile
	if estimator != nil {
		if estimator.EstimatorID() == "" || estimator.Version() == "" {
			return nil, fmt.Errorf("sqlstore: statistics token estimator profile is invalid")
		}
		copy := *estimator
		profile = &copy
	}
	return &ScopedStatistics{
		database: database, scopeID: scopeID, memoryArtifactID: memoryArtifactID,
		artifacts: artifacts, repository: repository, estimator: profile,
	}, nil
}

func (s *ScopedStatistics) Overview(ctx context.Context, period stats.Period, asOf time.Time) (stats.Statistics, error) {
	resolved, err := stats.ResolvePeriod(period, asOf)
	if err != nil {
		return stats.Statistics{}, err
	}
	var inventory stats.InventoryCounts
	var processed int64
	var usage []stats.StoredModelUsage
	var recall []stats.StoredRecallTokenUsage
	err = s.database.Transaction(ctx, func(tx DBTX) error {
		var err error
		inventory, err = s.repository.Inventory(ctx, tx, s.scopeID)
		if err != nil {
			return err
		}
		inventory.MemoryEntries, err = s.repository.MemoryEntryStates(ctx, tx, s.scopeID, s.memoryArtifactID, s.artifacts)
		if err != nil {
			return err
		}
		cursor, found, err := s.cursors.Load(ctx, tx, s.scopeID, trigger.SourceWindowName)
		if err != nil {
			return err
		}
		if found {
			processed = cursor.Cursor.Sequence()
		}
		usage, err = s.repository.Usage(ctx, tx, s.scopeID, resolved.StartDate(), resolved.EndDate())
		if err != nil {
			return err
		}
		if s.estimator != nil {
			recall, err = s.repository.RecallUsage(ctx, tx, s.scopeID, resolved.StartDate(), resolved.EndDate(), *s.estimator)
		}
		return err
	})
	if err != nil {
		return stats.Statistics{}, err
	}
	return stats.Build(s.scopeID, asOf, period, processed, inventory, usage, s.estimator, recall)
}

func (s *ScopedStatistics) Record(
	ctx context.Context,
	purpose stats.ModelPurpose,
	operation stats.ModelOperation,
	usage inference.Usage,
	usageDate time.Time,
) error {
	return s.database.Transaction(ctx, func(tx DBTX) error {
		return s.repository.Record(ctx, tx, s.scopeID, usageDate, purpose, operation, usage)
	})
}

func (s *ScopedStatistics) RecordRecall(
	ctx context.Context,
	measurement stats.RecallTokenMeasurement,
	usageDate time.Time,
) error {
	if s.estimator == nil || *s.estimator != measurement.Estimator() {
		return fmt.Errorf("recall measurement estimator does not match the deployment profile")
	}
	return s.database.Transaction(ctx, func(tx DBTX) error {
		return s.repository.RecordRecall(ctx, tx, s.scopeID, usageDate, measurement)
	})
}
