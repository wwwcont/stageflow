package observability

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"stageflow/internal/config"
)

type Components struct {
	Logger   *zap.Logger
	Metrics  *Metrics
	Shutdown func(context.Context) error
}

func Setup(ctx context.Context, cfg config.Config) (Components, error) {
	logger, err := newZapLogger(cfg)
	if err != nil {
		return Components{}, fmt.Errorf("build zap logger: %w", err)
	}
	traceShutdown, err := setupTracing(ctx, cfg)
	if err != nil {
		_ = logger.Sync()
		return Components{}, fmt.Errorf("setup tracing: %w", err)
	}
	metrics := NewMetrics()

	shutdown := func(ctx context.Context) error {
		return errors.Join(traceShutdown(ctx), syncLogger(logger))
	}
	return Components{Logger: logger, Metrics: metrics, Shutdown: shutdown}, nil
}

func syncLogger(logger *zap.Logger) error {
	if logger == nil {
		return nil
	}
	err := logger.Sync()
	if err != nil && strings.Contains(err.Error(), "invalid argument") {
		return nil
	}
	return err
}
