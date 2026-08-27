package server

import (
	"context"
	"errors"
	"time"

	servermetrics "github.com/ob-labs/powercontext-go/internal/observability/metrics"
	servertracing "github.com/ob-labs/powercontext-go/internal/observability/tracing"
	pcruntime "github.com/ob-labs/powercontext-go/internal/runtime"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
	"go.opentelemetry.io/otel/trace"
)

type applicationFoundation struct {
	storage              applicationStorage
	lifecycle            *pcruntime.Runtime
	assembled            assembledDependencies
	statisticsRepository sqlstore.StatisticsRepository
	statisticsClock      func() time.Time
	metrics              *servermetrics.Server
	tracing              trace.TracerProvider
}

func openApplicationFoundation(
	ctx context.Context,
	config ProcessConfig,
	dependencies Dependencies,
	storage applicationStorage,
) (applicationFoundation, error) {
	warnIfEphemeralMainDatabase(ctx, config, dependencies.Logger)
	tracingProvider := dependencies.TracerProvider
	var tracingResource *servertracing.Server
	var err error
	if tracingProvider == nil {
		tracingResource, err = servertracing.Configure(ctx, config.Tracing.Enabled)
		if err != nil {
			return applicationFoundation{}, errors.Join(err, storage.resource.Close(ctx))
		}
		tracingProvider = tracingResource.Provider()
	}
	metrics, err := configuredMetrics(config.Metrics.Enabled)
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var closeErrors []error
		if tracingResource != nil {
			closeErrors = append(closeErrors, tracingResource.Close(closeCtx))
		}
		closeErrors = append(closeErrors, storage.resource.Close(closeCtx))
		return applicationFoundation{}, errors.Join(append([]error{err}, closeErrors...)...)
	}
	assembled, assemblyErr := assembleDependencies(config, dependencies, tracingProvider)
	statisticsRepository, statisticsErr := sqlstore.NewStatisticsRepository(storage.dialect)
	statisticsClock := dependencies.Clock
	if statisticsClock == nil {
		statisticsClock = time.Now
	}
	ownedResources := make([]pcruntime.Resource, 0, 2+len(assembled.resources))
	ownedResources = append(ownedResources, storage.resource)
	if tracingResource != nil {
		ownedResources = append(ownedResources, tracingResource)
	}
	ownedResources = append(ownedResources, assembled.resources...)
	var scopeObserver pcruntime.ScopeCacheObserver
	if metrics != nil {
		scopeObserver = metrics.SetRuntimeScopes
	}
	lifecycle, lifecycleErr := pcruntime.NewConfigured(pcruntime.RuntimeOptions{
		ScopeCacheSize: config.Runtime.ScopeCacheSize,
		ScopeObserver:  scopeObserver,
		Tracing:        newRuntimeStageTracing(tracingProvider),
	}, relationalModelUsageRecorder{
		database: storage.database, repository: statisticsRepository,
		clock: statisticsClock, logger: dependencies.Logger,
	}, ownedResources...)
	if lifecycleErr != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var closeErrors []error
		for index := len(ownedResources) - 1; index >= 0; index-- {
			if ownedResources[index] != nil {
				closeErrors = append(closeErrors, ownedResources[index].Close(closeCtx))
			}
		}
		return applicationFoundation{}, errors.Join(append([]error{lifecycleErr}, closeErrors...)...)
	}
	foundation := applicationFoundation{
		storage: storage, lifecycle: lifecycle, assembled: assembled,
		statisticsRepository: statisticsRepository, statisticsClock: statisticsClock,
		metrics: metrics, tracing: tracingProvider,
	}
	if assemblyErr != nil {
		return applicationFoundation{}, foundation.closeWithError(assemblyErr)
	}
	if statisticsErr != nil {
		return applicationFoundation{}, foundation.closeWithError(statisticsErr)
	}
	return foundation, nil
}

func (f applicationFoundation) closeWithError(cause error) error {
	closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(cause, f.lifecycle.Close(closeCtx))
}
