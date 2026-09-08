package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/WithSoull/avito-kitchen/internal/platform/app"
	"github.com/WithSoull/avito-kitchen/internal/platform/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	if err := app.Run(ctx, cfg, logger); err != nil {
		stop()
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}

	stop()
}
