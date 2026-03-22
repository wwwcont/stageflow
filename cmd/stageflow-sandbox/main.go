package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"stageflow/internal/sandbox"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("STAGEFLOW_SANDBOX_ADDR")
	if addr == "" {
		addr = ":8091"
	}
	logLevel := os.Getenv("STAGEFLOW_SANDBOX_LOG_LEVEL")
	app, err := sandbox.NewApp(addr, logLevel)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bootstrap sandbox: %v\n", err)
		os.Exit(1)
	}
	if err := app.Run(ctx); err != nil {
		app.Logger().Error("run sandbox", zap.Error(err))
		os.Exit(1)
	}
}
