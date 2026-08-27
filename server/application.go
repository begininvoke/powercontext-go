package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/artifact/experience"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/artifact/skill"
	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/internal/endpoint"
	serverlogging "github.com/ob-labs/powercontext-go/internal/observability/logging"
	servermetrics "github.com/ob-labs/powercontext-go/internal/observability/metrics"
	servertracing "github.com/ob-labs/powercontext-go/internal/observability/tracing"
	"github.com/ob-labs/powercontext-go/internal/scheduler"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
	sqlstoreoceanbase "github.com/ob-labs/powercontext-go/internal/sqlstore/oceanbase"
	"github.com/ob-labs/powercontext-go/internal/webui"
	"github.com/ob-labs/powercontext-go/review"
	pcruntime "github.com/ob-labs/powercontext-go/runtime"
	"go.opentelemetry.io/otel/trace"
)

// Application is one fully assembled Runtime and its shared endpoint adapter.
// Close is idempotent and drains admitted work before closing persistence.
type Application struct {
	config            ProcessConfig
	runtime           *pcruntime.Runtime
	endpoint          *endpoint.Handler
	capabilities      pcruntime.Capabilities
	readiness         *pcruntime.ReadinessChecks
	metrics           *servermetrics.Server
	tracing           trace.TracerProvider
	logger            *slog.Logger
	review            *pcruntime.ReviewApplication
	externalSkills    *pcruntime.ExternalSkillApplication
	agentSkillTargets []skill.AgentSkillTarget

	readinessMu   sync.Mutex
	hasReadiness  bool
	lastReadiness pcruntime.ReadinessStatus
}

// OpenApplication initializes persistence projections before making any
// operation reachable. A partially initialized application is always closed.
func OpenApplication(ctx context.Context, config ProcessConfig, dependencies Dependencies) (*Application, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	var database *sqlstore.Database
	var databaseResource pcruntime.Resource
	dialect := sqlstore.SQLiteDialect
	var err error
	switch config.Database.Kind {
	case "sqlite":
		var dsn string
		dsn, err = SQLiteDSN(config.Database.SQLite.URL)
		if err == nil {
			database, err = sqlstore.OpenSQLite(ctx, sqlstore.SQLiteConfig{
				DSN: dsn, BusyTimeout: config.Database.SQLite.BusyTimeout,
				JournalMode: config.Database.SQLite.JournalMode, ForeignKeys: config.Database.SQLite.ForeignKeys,
				MaxOpenConns: config.Database.SQLite.MaxOpenConns, MaxIdleConns: config.Database.SQLite.MaxIdleConns,
				ConnMaxLifetime: config.Database.SQLite.ConnMaxLifetime,
			})
		}
		databaseResource = database
	case "oceanbase":
		dialect = sqlstore.MySQLDialect
		database, err = sqlstore.OpenOceanBase(ctx, sqlstore.OceanBaseConfig{
			URL:             config.Database.OceanBase.URL,
			MaxOpenConns:    config.Database.OceanBase.MaxOpenConns,
			MaxIdleConns:    config.Database.OceanBase.MaxIdleConns,
			ConnMaxLifetime: config.Database.OceanBase.MaxLifetime,
		})
		databaseResource = database
	case "seekdb":
		dialect = sqlstore.MySQLDialect
		var instance seekDBInstance
		database, instance.value, err = sqlstore.OpenSeekDB(ctx, sqlstore.SeekDBConfig{
			Path: config.Database.SeekDB.Path, Database: config.Database.SeekDB.Database,
			LibraryPath:     config.Database.SeekDB.LibraryPath,
			MaxOpenConns:    config.Database.SeekDB.MaxOpenConns,
			MaxIdleConns:    config.Database.SeekDB.MaxIdleConns,
			ConnMaxLifetime: config.Database.SeekDB.MaxLifetime,
		})
		if err == nil {
			instance.database = database
			databaseResource = &instance
		}
	}
	if err != nil {
		return nil, fmt.Errorf("server: open database: %w", err)
	}
	warnIfEphemeralMainDatabase(ctx, config, dependencies.Logger)
	tracingProvider := dependencies.TracerProvider
	var tracingResource *servertracing.Server
	if tracingProvider == nil {
		tracingResource, err = servertracing.Configure(ctx, config.Tracing.Enabled)
		if err != nil {
			return nil, errors.Join(err, databaseResource.Close(ctx))
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
		closeErrors = append(closeErrors, databaseResource.Close(closeCtx))
		return nil, errors.Join(append([]error{err}, closeErrors...)...)
	}
	assembled, err := assembleDependencies(config, dependencies, tracingProvider)
	statisticsRepository, statisticsErr := sqlstore.NewStatisticsRepository(dialect)
	statisticsClock := dependencies.Clock
	if statisticsClock == nil {
		statisticsClock = time.Now
	}
	ownedResources := make([]pcruntime.Resource, 0, 2+len(assembled.resources))
	ownedResources = append(ownedResources, databaseResource)
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
		database: database, repository: statisticsRepository,
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
		return nil, errors.Join(append([]error{lifecycleErr}, closeErrors...)...)
	}
	fail := func(cause error) (*Application, error) {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return nil, errors.Join(cause, lifecycle.Close(closeCtx))
	}
	if err != nil {
		return fail(err)
	}
	if statisticsErr != nil {
		return fail(statisticsErr)
	}
	artifacts, err := sqlstore.NewArtifactRepository(
		dialect,
		sqlstore.MemoryArtifactCodec(), sqlstore.ExperienceArtifactCodec(),
		sqlstore.SkillArtifactCodec(), sqlstore.HandoffArtifactCodec(),
	)
	if err != nil {
		return fail(err)
	}
	sources, err := sqlstore.NewSourceRepository(
		dialect,
		sqlstore.ContentSourceCodec(), sqlstore.ExternalSkillSnapshotSourceCodec(),
	)
	if err != nil {
		return fail(err)
	}
	candidates, err := sqlstore.NewCandidateRepository(
		dialect, sqlstore.ExperienceArtifactCodec(), sqlstore.SkillArtifactCodec(),
	)
	if err != nil {
		return fail(err)
	}

	var memoryIndexes []sqlstore.MemoryIndex
	var experienceIndex sqlstore.ExperienceIndex
	if dialect == sqlstore.SQLiteDialect {
		memoryIndexes = append(memoryIndexes, sqlstore.SQLiteMemoryFTSIndex{})
		experienceIndex = sqlstore.SQLiteExperienceFTSIndex{}
		if assembled.embeddingModel != nil {
			profile := assembled.embeddingModel.Profile()
			memoryProfile, buildErr := memory.NewEmbeddingProfile(
				profile.ID(), profile.ModelName(), profile.DimensionCount(), profile.NormalizationMode(),
			)
			if buildErr != nil {
				return fail(buildErr)
			}
			vectorIndex, buildErr := sqlstore.NewSQLiteMemoryVectorIndex(memoryProfile)
			if buildErr != nil {
				return fail(buildErr)
			}
			memoryIndexes = append(memoryIndexes, vectorIndex)
		}
	} else {
		memoryIndexes = append(memoryIndexes, sqlstoreoceanbase.MemoryFTSIndex{})
		experienceIndex = sqlstoreoceanbase.ExperienceFTSIndex{}
		if assembled.embeddingModel != nil {
			profile := assembled.embeddingModel.Profile()
			memoryProfile, buildErr := memory.NewEmbeddingProfile(
				profile.ID(), profile.ModelName(), profile.DimensionCount(), profile.NormalizationMode(),
			)
			if buildErr != nil {
				return fail(buildErr)
			}
			vectorIndex, buildErr := sqlstoreoceanbase.NewMemoryVectorIndex(memoryProfile)
			if buildErr != nil {
				return fail(buildErr)
			}
			memoryIndexes = append(memoryIndexes, vectorIndex)
		}
	}
	memoryIndex, err := sqlstore.NewCompositeMemoryIndex(memoryIndexes...)
	if err != nil {
		return fail(err)
	}
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		if err := memoryIndex.Initialize(ctx, tx); err != nil {
			return err
		}
		return experienceIndex.Initialize(ctx, tx)
	}); err != nil {
		return fail(fmt.Errorf("server: initialize search projections: %w", err))
	}

	sourceBackend, err := sqlstore.NewRuntimeSourceBackend(database, sources)
	if err != nil {
		return fail(err)
	}
	sourceApplication, err := pcruntime.NewSourceApplication(lifecycle, sourceBackend)
	if err != nil {
		return fail(err)
	}

	idFactory := dependencies.IDFactory
	if idFactory == nil {
		idFactory = scopedIDFactory
	}
	memoryReranker := pcruntime.TraceMemoryReranker(lifecycle, assembled.memoryReranker)
	memoryFactory := func(scopeID string) (*memory.Service, error) {
		repository, buildErr := sqlstore.NewMemoryRepository(database, scopeID, artifacts, memoryIndex)
		if buildErr != nil {
			return nil, buildErr
		}
		resolver, buildErr := sqlstore.NewMemorySourceResolver(database, scopeID, sources)
		if buildErr != nil {
			return nil, buildErr
		}
		return memory.NewService(repository, memory.ServiceOptions{
			CandidatePipeline:    assembled.memoryCandidates,
			EmbeddingModel:       assembled.embeddingModel,
			Reranker:             memoryReranker,
			RerankCandidateLimit: config.Runtime.MemoryRerankCandidateLimit,
			SourceResolver:       resolver, IDFactory: idFactory, Clock: dependencies.Clock,
		})
	}
	flushFactory := func(scopeID string) (pcruntime.MemoryFlushBackend, error) {
		repository, buildErr := sqlstore.NewMemoryRepository(database, scopeID, artifacts, memoryIndex)
		if buildErr != nil {
			return nil, buildErr
		}
		return sqlstore.NewMemoryFlushStore(database, scopeID, sources, repository)
	}
	memoryApplication, err := pcruntime.NewMemoryApplicationWithFlush(
		lifecycle, memoryFactory, flushFactory,
		pcruntime.DefaultMemoryArtifactID, config.Runtime.SourceWindowLimit,
	)
	if err != nil {
		return fail(err)
	}
	experienceRecall := pcruntime.ExperienceRecallFunc(func(
		ctx context.Context, scopeID, query string, limit int,
	) ([]experience.SearchHit, error) {
		var result []experience.SearchHit
		err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
			var searchErr error
			result, searchErr = experienceIndex.Search(ctx, tx, scopeID, query, limit)
			return searchErr
		})
		return result, err
	})
	tokenEstimator := inference.CharacterTokenEstimator()
	contextApplication, err := pcruntime.NewContextApplicationWithRecall(
		lifecycle, memoryApplication, experienceRecall,
		relationalRecallStatistics{
			database: database, sources: sources, artifacts: artifacts,
			repository: statisticsRepository, estimator: tokenEstimator,
			clock: statisticsClock, logger: dependencies.Logger,
		},
	)
	if err != nil {
		return fail(err)
	}

	reviewFactory := func(scopeID string) (*review.Service, error) {
		backend, buildErr := sqlstore.NewReviewBackend(database, scopeID, candidates, artifacts, sources, experienceIndex)
		if buildErr != nil {
			return nil, buildErr
		}
		return review.NewService(backend, idFactory)
	}
	reviewApplication, err := pcruntime.NewReviewApplication(lifecycle, reviewFactory)
	if err != nil {
		return fail(err)
	}
	generationFactory := func(scopeID string) (*review.GenerationService, error) {
		service, buildErr := reviewFactory(scopeID)
		if buildErr != nil {
			return nil, buildErr
		}
		evidence, buildErr := sqlstore.NewGenerationEvidenceReader(database, scopeID, sources, artifacts)
		if buildErr != nil {
			return nil, buildErr
		}
		return review.NewGenerationService(
			evidence, service, assembled.experienceGenerator, assembled.skillGenerator,
		)
	}
	generationApplication, err := pcruntime.NewGenerationApplication(lifecycle, generationFactory)
	if err != nil {
		return fail(err)
	}
	var experienceIncubationApplication *pcruntime.ExperienceIncubationApplication
	if assembled.experienceCandidates != nil {
		experienceIncubationApplication, err = pcruntime.NewExperienceIncubationApplication(
			lifecycle,
			func(scopeID string) (pcruntime.ExperienceIncubationBackend, error) {
				return sqlstore.NewExperienceIncubationStore(database, scopeID, sources, candidates)
			},
			assembled.experienceCandidates,
			idFactory,
		)
		if err != nil {
			return fail(err)
		}
	}

	var externalApplication *pcruntime.ExternalSkillApplication
	if assembled.externalSkills != nil {
		snapshots, buildErr := sqlstore.NewExternalSkillSnapshotStore(database, sources)
		if buildErr != nil {
			return fail(buildErr)
		}
		externalApplication, err = pcruntime.NewExternalSkillApplication(
			lifecycle,
			func(scopeID string) (*skill.RegistryService, error) {
				store, buildErr := sqlstore.NewExternalSkillStore(database, scopeID)
				if buildErr != nil {
					return nil, buildErr
				}
				return skill.NewRegistryService(store, assembled.externalSkills)
			},
			generationFactory, snapshots,
		)
		if err != nil {
			return fail(err)
		}
	}

	handoffFactory := func(scopeID string) (*handoff.Service, error) {
		memoryService, buildErr := memoryFactory(scopeID)
		if buildErr != nil {
			return nil, buildErr
		}
		backend, buildErr := sqlstore.NewHandoffBackend(database, scopeID, artifacts)
		if buildErr != nil {
			return nil, buildErr
		}
		resolver, buildErr := sqlstore.NewHandoffEvidenceResolver(database, scopeID, sources, artifacts, memoryService)
		if buildErr != nil {
			return nil, buildErr
		}
		return handoff.NewService(
			scopeID, pcruntime.DefaultHandoffArtifactID, backend, resolver, assembled.handoffGenerator,
		)
	}
	activationStore, err := sqlstore.NewHandoffActivationStore(database, sources)
	if err != nil {
		return fail(err)
	}
	if config.Runtime.SourceWindowInterval != nil || config.Runtime.ExperienceIncubationInterval != nil {
		if config.Runtime.ExperienceIncubationInterval != nil && experienceIncubationApplication == nil {
			return fail(errors.New("server: scheduled Experience incubation pipeline is not configured"))
		}
		backgroundLogger := namedLogger(dependencies.Logger, "powercontext.builtin.runtime.application")
		processor, buildErr := pcruntime.NewScheduledProcessor(
			lifecycle, sourceBackend, memoryApplication, experienceIncubationApplication,
			scheduledObserver(backgroundLogger), dependencies.Clock,
		)
		if buildErr != nil {
			return fail(buildErr)
		}
		scheduled, buildErr := scheduler.Open(ctx, scheduler.Config{
			Path:                         config.SchedulerPath,
			SourceWindowInterval:         config.Runtime.SourceWindowInterval,
			ExperienceIncubationInterval: config.Runtime.ExperienceIncubationInterval,
			SourceWindow:                 processor.ProcessSourceWindows,
			ExperienceIncubation:         processor.IncubateExperiences,
			OnError:                      scheduledRunErrorObserver(backgroundLogger), Clock: dependencies.Clock,
		})
		if buildErr != nil {
			return fail(buildErr)
		}
		if buildErr = lifecycle.AttachScheduler(scheduled); buildErr != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			closeErr := scheduled.Close(closeCtx)
			cancel()
			return fail(errors.Join(buildErr, closeErr))
		}
	}
	handoffApplication, err := pcruntime.NewHandoffApplication(lifecycle, handoffFactory, activationStore)
	if err != nil {
		return fail(err)
	}
	workApplication, err := pcruntime.NewWorkApplication(lifecycle, sourceBackend, handoffFactory)
	if err != nil {
		return fail(err)
	}

	var handoffReportApplication *pcruntime.HandoffReportApplication
	if config.HandoffReport.Enabled {
		reportStore, buildErr := sqlstore.NewHandoffReportStore(database, dialect)
		if buildErr != nil {
			return fail(buildErr)
		}
		if buildErr = reportStore.EnsureSchema(ctx); buildErr != nil {
			return fail(buildErr)
		}
		reader, buildErr := pcruntime.NewHandoffReportReader(handoffFactory)
		if buildErr != nil {
			return fail(buildErr)
		}
		handoffReportApplication, err = pcruntime.NewHandoffReportApplication(
			lifecycle, reportStore, reader, workApplication, dependencies.Clock, nil,
			func(ctx context.Context) ([]string, error) { return sqlstore.HandoffScopeIDs(ctx, database) },
		)
		if err != nil {
			return fail(err)
		}
	}

	tokenEstimatorProfile := tokenEstimator.Profile()
	statisticsApplication, err := pcruntime.NewStatisticsApplication(
		lifecycle,
		func(scopeID string) (pcruntime.StatisticsReader, error) {
			return sqlstore.NewScopedStatistics(
				database, scopeID, pcruntime.DefaultMemoryArtifactID,
				artifacts, statisticsRepository, &tokenEstimatorProfile,
			)
		},
		statisticsClock,
	)
	if err != nil {
		return fail(err)
	}

	readiness, err := configuredReadiness(database.Ping, assembled, statisticsClock)
	if err != nil {
		return fail(err)
	}
	capabilities, err := runtimeCapabilities(assembled, memoryIndex.Capabilities())
	if err != nil {
		return fail(err)
	}
	application := &Application{
		config: config, runtime: lifecycle, capabilities: capabilities,
		readiness: readiness, metrics: metrics, tracing: tracingProvider, logger: dependencies.Logger,
		review: reviewApplication, externalSkills: externalApplication,
		agentSkillTargets: assembled.agentSkillTargets,
	}
	application.endpoint = endpoint.NewHandler(endpoint.HandlerOptions{
		Capabilities: application.getCapabilities,
		Readiness:    application.getReadiness,
		Sources:      sourceApplication, Memory: memoryApplication, Context: contextApplication,
		Review: reviewApplication, Generation: generationApplication, External: externalApplication,
		Handoff: handoffApplication, Work: workApplication, HandoffReport: handoffReportApplication,
		Statistics: statisticsApplication,
	})
	if _, err := application.getReadiness(ctx); err != nil {
		return fail(err)
	}
	return application, nil
}

func (a *Application) Endpoint() v1.Handler { return a.endpoint }

func (a *Application) Runtime() *pcruntime.Runtime { return a.runtime }

func (a *Application) Metrics() *servermetrics.Server { return a.metrics }

func (a *Application) HTTPHandler() (http.Handler, error) {
	if a == nil || a.endpoint == nil {
		return nil, errors.New("server: application is not initialized")
	}
	token := ""
	if a.config.Auth.Enabled {
		token = a.config.Auth.Token
	}
	var webOptions *webui.Options
	if a.config.Dashboard.Enabled || a.config.HandoffReport.Enabled {
		scopes := make([]webui.Scope, len(a.config.Dashboard.Scopes))
		for index, scope := range a.config.Dashboard.Scopes {
			scopes[index] = webui.Scope{ScopeID: scope.ScopeID, DisplayName: scope.DisplayName}
		}
		webOptions = &webui.Options{
			DashboardEnabled: a.config.Dashboard.Enabled, Scopes: scopes,
			HandoffReportEnabled:   a.config.HandoffReport.Enabled,
			AuthenticationRequired: a.config.Auth.Enabled,
			AgentSkillTargets:      append([]skill.AgentSkillTarget(nil), a.agentSkillTargets...),
			SkillProjections:       webSkillProjectionOperations{review: a.review, external: a.externalSkills},
		}
	}
	return NewHTTPHandler(a.endpoint, HTTPOptions{
		BearerToken: token, HandoffReportRoutes: a.config.HandoffReport.Enabled,
		Metrics: a.metrics, TracerProvider: a.tracing, Logger: a.logger, AccessLog: a.config.Logging.Access,
		MCP:   MCPOptions{Enabled: a.config.MCP.Enabled, Path: a.config.MCP.Path},
		WebUI: webOptions,
	})
}

func (a *Application) Close(ctx context.Context) error {
	if a == nil || a.runtime == nil {
		return nil
	}
	if a.metrics != nil {
		a.metrics.SetReady(false)
	}
	return a.runtime.Close(ctx)
}

func (a *Application) getCapabilities(ctx context.Context) (pcruntime.Capabilities, error) {
	var result pcruntime.Capabilities
	err := a.runtime.Operation(ctx, func(context.Context) error {
		result = a.capabilities
		return nil
	})
	return result, err
}

func (a *Application) getReadiness(ctx context.Context) (pcruntime.Readiness, error) {
	var value pcruntime.Readiness
	err := a.runtime.Operation(ctx, func(ctx context.Context) error {
		var runErr error
		value, runErr = a.readiness.Run(ctx)
		return runErr
	})
	if a.metrics != nil {
		a.metrics.SetReady(err == nil && value.Status() != pcruntime.NotReady)
	}
	if err == nil {
		a.observeReadiness(ctx, value.Status())
	}
	return value, err
}

func configuredReadiness(
	database pcruntime.DependencyOperation,
	dependencies assembledDependencies,
	clock pcruntime.Clock,
) (*pcruntime.ReadinessChecks, error) {
	definitions := []pcruntime.ProbeDefinition{
		{
			Name: "runtime",
			Probe: func(context.Context) (pcruntime.CheckStatus, error) {
				return pcruntime.CheckReady, nil
			},
		},
		{
			Name: "database", Blocking: true,
			Probe: pcruntime.DependencyProbe(database, pcruntime.DefaultReadinessProbeTimeout),
		},
	}
	for _, configured := range []struct {
		name      string
		operation pcruntime.DependencyOperation
	}{
		{name: "inference.generation", operation: dependencies.generationReadiness},
		{name: "inference.embedding", operation: dependencies.embeddingReadiness},
	} {
		if configured.operation == nil {
			continue
		}
		probe := pcruntime.DependencyProbe(configured.operation, pcruntime.DefaultReadinessProbeTimeout)
		cached, err := pcruntime.NewCachedProbe(
			probe,
			pcruntime.DefaultReadinessCacheTTL,
			pcruntime.TransientReadinessCacheTTL,
			clock,
		)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, pcruntime.ProbeDefinition{
			Name: configured.name, Probe: cached.Probe,
		})
	}
	return pcruntime.NewReadinessChecks(definitions)
}

func (a *Application) observeReadiness(ctx context.Context, status pcruntime.ReadinessStatus) {
	if a == nil {
		return
	}
	a.readinessMu.Lock()
	changed := !a.hasReadiness || a.lastReadiness != status
	a.hasReadiness = true
	a.lastReadiness = status
	a.readinessMu.Unlock()
	if !changed {
		return
	}
	event, message := "server.not_ready", "PowerContext Server is not ready"
	switch status {
	case pcruntime.Ready:
		event, message = "server.ready", "PowerContext Server is ready"
	case pcruntime.Degraded:
		event, message = "server.degraded", "PowerContext Server is degraded"
	}
	serverlogging.LogLifecycle(ctx, namedLogger(a.logger, "powercontext.server.factory"), event, message)
}
