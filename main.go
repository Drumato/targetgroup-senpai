package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/Drumato/targetgroup-senpai/config"
	"github.com/Drumato/targetgroup-senpai/manager"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := config.LoadConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	var logLevel slog.Level

	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	restConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.ErrorContext(ctx, "Failed to get in-cluster config", "error", err)
		os.Exit(1)
	}
	k8sClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		logger.ErrorContext(ctx, "Failed to create k8s client", "error", err)
		os.Exit(1)
	}

	m := manager.NewManager(cfg, k8sClient, logger)

	if err := m.Start(ctx); err != nil {
		logger.ErrorContext(ctx, "Manager exited with error", "error", err)
		os.Exit(1)
	}
}
