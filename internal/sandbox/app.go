package sandbox

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type App struct {
	server *http.Server
	logger *zap.Logger
}

func NewApp(addr, logLevel string) (*App, error) {
	if strings.TrimSpace(addr) == "" {
		addr = ":8091"
	}
	logger, err := newLogger(logLevel)
	if err != nil {
		return nil, err
	}
	store := NewStore()
	handler := NewServer(store, logger)
	return &App{
		server: &http.Server{Addr: addr, Handler: handler.Handler(), ReadHeaderTimeout: 5 * time.Second},
		logger: logger,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("starting stageflow sandbox", zap.String("addr", a.server.Addr))
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown sandbox: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func (a *App) Logger() *zap.Logger {
	return a.logger
}

func newLogger(rawLevel string) (*zap.Logger, error) {
	level := zap.InfoLevel
	if strings.TrimSpace(rawLevel) == "" {
		rawLevel = "info"
	}
	switch strings.ToLower(strings.TrimSpace(rawLevel)) {
	case "debug":
		level = zap.DebugLevel
	case "info":
		level = zap.InfoLevel
	case "warn":
		level = zap.WarnLevel
	case "error":
		level = zap.ErrorLevel
	default:
		return nil, fmt.Errorf("unsupported sandbox log level %q", rawLevel)
	}
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(level)
	cfg.OutputPaths = []string{"stdout"}
	cfg.ErrorOutputPaths = []string{"stderr"}
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.MessageKey = "msg"
	cfg.EncoderConfig.CallerKey = "caller"
	cfg.InitialFields = map[string]any{"service": "stageflow-sandbox", "pid": os.Getpid()}
	return cfg.Build(zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}
