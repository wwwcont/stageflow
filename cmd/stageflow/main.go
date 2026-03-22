package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"stageflow/internal/app"
	"stageflow/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(ctx, os.Args[1:], config.EnvSource{})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	application, err := app.New(ctx, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bootstrap app: %v\n", err)
		os.Exit(1)
	}

	if err := application.Run(ctx); err != nil {
		application.Logger().Error("run app", zap.Error(err))
		os.Exit(1)
	}
}
