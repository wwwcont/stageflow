package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"time"

	"go.uber.org/zap"

	"stageflow/internal/config"
	uihttp "stageflow/internal/delivery/http/ui"
	"stageflow/internal/observability"
	"stageflow/internal/usecase"
)

type Server struct {
	server *stdhttp.Server
	logger *zap.Logger
}

func NewServer(cfg config.Config, logger *zap.Logger, metrics *observability.Metrics, workspaceManagement usecase.WorkspaceManagementUseCase, savedRequestManagement usecase.SavedRequestManagementUseCase, flowManagement usecase.FlowManagementUseCase, runService usecase.RunService, curlImport usecase.CurlImportUseCase, runEvents usecase.RunEventUseCase) *Server {
	mux := stdhttp.NewServeMux()
	h := newHandler(services{
		workspaceManagement:    workspaceManagement,
		savedRequestManagement: savedRequestManagement,
		flowManagement:         flowManagement,
		runService:             runService,
		curlImport:             curlImport,
	})
	mux.HandleFunc("GET /healthz", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		writeJSON(w, stdhttp.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		writeJSON(w, stdhttp.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /version", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		writeJSON(w, stdhttp.StatusOK, map[string]string{
			"service": cfg.ServiceName,
			"env":     cfg.Environment,
			"version": cfg.Metadata.Version,
		})
	})
	mux.HandleFunc("POST /api/v1/workspaces", h.createWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces", h.listWorkspaces)
	mux.HandleFunc("GET /api/v1/workspaces/{id}", h.getWorkspace)
	mux.HandleFunc("PUT /api/v1/workspaces/{id}", h.updateWorkspace)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/archive", h.archiveWorkspace)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/unarchive", h.unarchiveWorkspace)
	mux.HandleFunc("PUT /api/v1/workspaces/{id}/policy", h.updateWorkspacePolicy)
	mux.HandleFunc("PUT /api/v1/workspaces/{id}/variables", h.updateWorkspaceVariables)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/secrets", h.putWorkspaceSecret)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/secrets", h.listWorkspaceSecrets)
	mux.HandleFunc("DELETE /api/v1/workspaces/{id}/secrets/{secretId}", h.deleteWorkspaceSecret)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/requests", h.createSavedRequest)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/requests", h.listSavedRequests)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/requests/{requestId}", h.getSavedRequest)
	mux.HandleFunc("PUT /api/v1/workspaces/{id}/requests/{requestId}", h.updateSavedRequest)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/requests/{requestId}/run", h.runSavedRequest)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/flows", h.createFlow)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/flows", h.listFlows)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/flows/{flowId}", h.getFlow)
	mux.HandleFunc("PUT /api/v1/workspaces/{id}/flows/{flowId}", h.updateFlow)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/flows/{flowId}/validate", h.validateFlow)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/flows/{flowId}/runs", h.launchRun)
	mux.HandleFunc("GET /api/v1/runs/{id}", h.getRun)
	mux.HandleFunc("GET /api/v1/runs/{id}/steps", h.getRunSteps)
	mux.HandleFunc("POST /api/v1/runs/{id}/rerun", h.rerun)
	mux.HandleFunc("POST /api/v1/import/curl", h.importCurl)
	if cfg.UI.Enabled {
		uiHandler := uihttp.NewHandler(cfg, logger, workspaceManagement, savedRequestManagement, flowManagement, runService, curlImport, runEvents)
		mux.Handle(cfg.UI.Prefix, uiHandler)
		mux.Handle(cfg.UI.Prefix+"/", uiHandler)
		mux.Handle("/static/", uiHandler)
	}
	if metrics != nil && cfg.Observability.MetricsPath != "" {
		mux.Handle(cfg.Observability.MetricsPath, metrics.Handler())
	}

	wrapped := newMiddleware(logger, metrics).chain(mux)
	return &Server{
		server: &stdhttp.Server{
			Addr:              cfg.HTTP.Address,
			Handler:           wrapped,
			ReadHeaderTimeout: minPositive(cfg.HTTP.ReadTimeout, time.Second),
			ReadTimeout:       cfg.HTTP.ReadTimeout,
			WriteTimeout:      cfg.HTTP.WriteTimeout,
			IdleTimeout:       60 * time.Second,
		},
		logger: logger,
	}
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server listening", zap.String("addr", s.server.Addr))
		if err := s.server.ListenAndServe(); err != nil && err != stdhttp.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func minPositive(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func writeJSON(w stdhttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
