package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"go.uber.org/zap"

	"stageflow/internal/config"
	deliveryhttp "stageflow/internal/delivery/http"
	"stageflow/internal/domain"
	"stageflow/internal/execution"
	"stageflow/internal/observability"
	"stageflow/internal/queue/redisqueue"
	"stageflow/internal/redisclient"
	"stageflow/internal/repository"
	postgresrepo "stageflow/internal/repository/postgres"
	"stageflow/internal/runevents"
	"stageflow/internal/usecase"
	"stageflow/internal/worker"
	"stageflow/pkg/clock"
)

type App struct {
	config   config.Config
	logger   *zap.Logger
	http     *deliveryhttp.Server
	worker   *worker.Service
	shutdown func(context.Context) error
}

type persistence struct {
	workspaces    repository.WorkspaceRepository
	savedRequests repository.SavedRequestRepository
	flows         repository.FlowRepository
	flowSteps     repository.FlowStepRepository
	runs          repository.RunRepository
	runSteps      repository.RunStepRepository
	runEvents     repository.RunEventRepository
	close         func() error
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	obs, err := observability.Setup(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("setup observability: %w", err)
	}

	persistenceLayer, err := initPersistence(ctx, cfg, obs.Metrics)
	if err != nil {
		return nil, fmt.Errorf("init persistence: %w", err)
	}
	runEventBroker := runevents.NewBroker()
	allowLoopbackHosts, allowIPHosts := executorHostPolicies(cfg.Runtime.AllowedHosts)
	httpExecutor, err := execution.NewSafeHTTPExecutor(execution.HTTPExecutorConfig{
		AllowedHosts:       cfg.Runtime.AllowedHosts,
		AllowLoopbackHosts: allowLoopbackHosts,
		AllowIPHosts:       allowIPHosts,
	})
	if err != nil {
		return nil, fmt.Errorf("build http executor: %w", err)
	}
	httpExecutor.SetLogger(obs.Logger)
	engine, err := execution.NewSequentialEngine(
		persistenceLayer.workspaces,
		persistenceLayer.savedRequests,
		persistenceLayer.flows,
		persistenceLayer.flowSteps,
		persistenceLayer.runs,
		persistenceLayer.runSteps,
		execution.NewDefaultTemplateRenderer(),
		httpExecutor,
		execution.NewDefaultExtractor(),
		execution.NewDefaultAsserter(),
		clock.System{},
	)
	if err != nil {
		return nil, fmt.Errorf("build execution engine: %w", err)
	}
	engine.SetObservability(obs.Logger, obs.Metrics)
	redisClient, err := redisclient.New(redisclient.Config{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err != nil {
		return nil, fmt.Errorf("build redis client: %w", err)
	}
	runQueue, err := redisqueue.New(redisClient, redisqueue.Config{Prefix: cfg.Redis.KeyPrefix})
	if err != nil {
		return nil, fmt.Errorf("build redis queue: %w", err)
	}
	dispatcher, err := worker.NewDispatcher(runQueue, clock.System{})
	if err != nil {
		return nil, fmt.Errorf("build dispatcher: %w", err)
	}
	backgroundWorker, err := worker.NewService(
		runQueue,
		persistenceLayer.runs,
		engine,
		obs.Logger,
		clock.System{},
		worker.Config{
			WorkerID:          cfg.Worker.ID,
			QueueName:         cfg.Runtime.DefaultRunQueue,
			PollInterval:      cfg.Worker.PollInterval,
			LeaseDuration:     cfg.Worker.LeaseDuration,
			HeartbeatInterval: cfg.Worker.HeartbeatInterval,
			StaleRunTimeout:   cfg.Worker.StaleRunTimeout,
			RequeueDelay:      cfg.Worker.RequeueDelay,
			RecoveryBatchSize: cfg.Worker.RecoveryBatchSize,
		},
	)
	if err != nil {
		if persistenceLayer.close != nil {
			_ = persistenceLayer.close()
		}
		return nil, fmt.Errorf("build worker: %w", err)
	}
	idFactory := usecase.NewMonotonicRunIDFactory(uint64(time.Now().UTC().UnixNano()))

	flowManagement, err := usecase.NewFlowManagementService(persistenceLayer.workspaces, persistenceLayer.savedRequests, persistenceLayer.flows, persistenceLayer.flowSteps, clock.System{})
	if err != nil {
		if persistenceLayer.close != nil {
			_ = persistenceLayer.close()
		}
		return nil, fmt.Errorf("build flow management service: %w", err)
	}
	workspaceManagement, err := usecase.NewWorkspaceManagementService(persistenceLayer.workspaces, clock.System{})
	if err != nil {
		if persistenceLayer.close != nil {
			_ = persistenceLayer.close()
		}
		return nil, fmt.Errorf("build workspace management service: %w", err)
	}
	savedRequestManagement, err := usecase.NewSavedRequestManagementService(persistenceLayer.workspaces, persistenceLayer.savedRequests, clock.System{})
	if err != nil {
		if persistenceLayer.close != nil {
			_ = persistenceLayer.close()
		}
		return nil, fmt.Errorf("build saved request management service: %w", err)
	}
	runService, err := usecase.NewRunCoordinator(persistenceLayer.workspaces, persistenceLayer.savedRequests, persistenceLayer.flows, persistenceLayer.flowSteps, persistenceLayer.runs, persistenceLayer.runSteps, dispatcher, idFactory, cfg.Runtime.DefaultRunQueue)
	if err != nil {
		if persistenceLayer.close != nil {
			_ = persistenceLayer.close()
		}
		return nil, fmt.Errorf("build flow service: %w", err)
	}
	curlImport, err := usecase.NewCurlImportService(usecase.NewDefaultCurlCommandParser())
	if err != nil {
		if persistenceLayer.close != nil {
			_ = persistenceLayer.close()
		}
		return nil, fmt.Errorf("build curl import service: %w", err)
	}

	runEventService, err := usecase.NewRunEventService(persistenceLayer.runs, persistenceLayer.runEvents, runEventBroker, clock.System{})
	if err != nil {
		if persistenceLayer.close != nil {
			_ = persistenceLayer.close()
		}
		return nil, fmt.Errorf("build run event service: %w", err)
	}
	engine.SetEventPublisher(runEventService)

	backgroundWorker.SetMetrics(obs.Metrics)
	httpServer := deliveryhttp.NewServer(cfg, obs.Logger, obs.Metrics, workspaceManagement, savedRequestManagement, flowManagement, runService, curlImport, runEventService)

	shutdown := combineShutdowns(obs.Shutdown, func(context.Context) error {
		if persistenceLayer.close == nil {
			return nil
		}
		return persistenceLayer.close()
	})

	return &App{config: cfg, logger: obs.Logger, http: httpServer, worker: backgroundWorker, shutdown: shutdown}, nil
}

func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if a.shutdown == nil {
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.shutdown(shutdownCtx)
	}()

	errCh := make(chan error, 2)
	go func() {
		errCh <- a.http.Run(runCtx)
	}()
	go func() {
		errCh <- a.worker.Run(runCtx)
	}()

	a.logger.Info("stageflow runtime started",
		zap.String("service", a.config.ServiceName),
		zap.String("env", a.config.Environment),
		zap.String("version", a.config.Metadata.Version),
		zap.String("http_addr", a.config.HTTP.Address),
		zap.String("queue", a.config.Runtime.DefaultRunQueue),
		zap.String("worker_id", a.config.Worker.ID),
		zap.Bool("tracing_enabled", a.config.Observability.Tracing.Enabled),
		zap.String("tracing_exporter", a.config.Observability.Tracing.Exporter),
		zap.Any("allowed_hosts", a.config.Runtime.AllowedHosts),
	)

	select {
	case <-runCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.config.HTTP.ShutdownTimeout)
		defer cancel()
		if err := a.http.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return nil
		}
		return runCtx.Err()
	case err := <-errCh:
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), a.config.HTTP.ShutdownTimeout)
		defer shutdownCancel()
		if shutdownErr := a.http.Shutdown(shutdownCtx); shutdownErr != nil {
			return errors.Join(err, shutdownErr)
		}
		return err
	}
}

func (a *App) Logger() *zap.Logger {
	return a.logger
}

func executorHostPolicies(allowedHosts []string) (allowLoopbackHosts, allowIPHosts bool) {
	for _, host := range allowedHosts {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized == "" {
			continue
		}
		if normalized == "localhost" {
			allowLoopbackHosts = true
			continue
		}
		if ip := net.ParseIP(normalized); ip != nil {
			allowIPHosts = true
			if ip.IsLoopback() {
				allowLoopbackHosts = true
			}
		}
	}
	return allowLoopbackHosts, allowIPHosts
}

func bootstrapWorkspace(cfg config.Config) domain.Workspace {
	now := time.Now().UTC()
	return domain.Workspace{
		ID:          "bootstrap",
		Name:        "Bootstrap workspace",
		Slug:        "bootstrap",
		Description: "Default workspace for the in-memory StageFlow runtime.",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy: domain.WorkspacePolicy{
			AllowedHosts:          cfg.Runtime.AllowedHosts,
			MaxSavedRequests:      100,
			MaxFlows:              100,
			MaxStepsPerFlow:       25,
			MaxRequestBodyBytes:   1 << 20,
			DefaultTimeoutMS:      10000,
			MaxRunDurationSeconds: 600,
			DefaultRetryPolicy:    domain.RetryPolicy{Enabled: false},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func initPersistence(ctx context.Context, cfg config.Config, metrics *observability.Metrics) (persistence, error) {
	switch cfg.Runtime.StorageBackend {
	case "postgres":
		db, err := sql.Open(cfg.Postgres.Driver, cfg.Postgres.DSN)
		if err != nil {
			return persistence{}, fmt.Errorf("open postgres database: %w", err)
		}
		db.SetMaxOpenConns(cfg.Postgres.MaxOpenConns)
		db.SetMaxIdleConns(cfg.Postgres.MaxIdleConns)
		db.SetConnMaxLifetime(cfg.Postgres.ConnMaxLifetime)
		if err := db.PingContext(ctx); err != nil {
			_ = db.Close()
			return persistence{}, fmt.Errorf("ping postgres database: %w", err)
		}
		workspaceRepo := postgresrepo.NewWorkspaceRepository(db)
		if err := ensureBootstrapWorkspace(ctx, workspaceRepo, bootstrapWorkspace(cfg)); err != nil {
			_ = db.Close()
			return persistence{}, err
		}
		return persistence{
			workspaces:    workspaceRepo,
			savedRequests: postgresrepo.NewSavedRequestRepository(db),
			flows:         observability.WrapFlowRepository(postgresrepo.NewFlowRepository(db)),
			flowSteps:     observability.WrapFlowStepRepository(postgresrepo.NewFlowStepRepository(db)),
			runs:          observability.WrapRunRepository(postgresrepo.NewRunRepository(db), metrics),
			runSteps:      observability.WrapRunStepRepository(postgresrepo.NewRunStepRepository(db)),
			runEvents:     postgresrepo.NewRunEventRepository(db),
			close:         db.Close,
		}, nil
	default:
		workspaceRepo := repository.NewInMemoryWorkspaceRepository(bootstrapWorkspace(cfg))
		return persistence{
			workspaces:    workspaceRepo,
			savedRequests: repository.NewInMemorySavedRequestRepository(),
			flows:         observability.WrapFlowRepository(repository.NewInMemoryFlowRepository()),
			flowSteps:     observability.WrapFlowStepRepository(repository.NewInMemoryFlowStepRepository()),
			runs:          observability.WrapRunRepository(repository.NewInMemoryRunRepository(), metrics),
			runSteps:      observability.WrapRunStepRepository(repository.NewInMemoryRunStepRepository()),
			runEvents:     repository.NewInMemoryRunEventRepository(),
		}, nil
	}
}

func ensureBootstrapWorkspace(ctx context.Context, repo repository.WorkspaceRepository, workspace domain.Workspace) error {
	if _, err := repo.GetByID(ctx, workspace.ID); err == nil {
		return nil
	} else {
		var notFound *domain.NotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("get bootstrap workspace: %w", err)
		}
	}
	if err := repo.Create(ctx, workspace); err != nil {
		var conflict *domain.ConflictError
		if errors.As(err, &conflict) {
			return nil
		}
		return fmt.Errorf("create bootstrap workspace: %w", err)
	}
	return nil
}

func combineShutdowns(items ...func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		var errs []error
		for _, item := range items {
			if item == nil {
				continue
			}
			if err := item(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}
