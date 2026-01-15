package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/Drumato/targetgroup-senpai/config"
	"github.com/Drumato/targetgroup-senpai/manager"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

	// Try to get in-cluster config first, fallback to kubeconfig if it fails
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.InfoContext(ctx, "Failed to get in-cluster config, trying kubeconfig", "error", err)

		// Try to load from default kubeconfig location
		restConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to load kubeconfig", "error", err)
			os.Exit(1)
		}

		logger.InfoContext(ctx, "Successfully loaded kubeconfig")
	} else {
		logger.InfoContext(ctx, "Successfully loaded in-cluster config")
	}
	k8sClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		logger.ErrorContext(ctx, "Failed to create k8s client", "error", err)
		os.Exit(1)
	}

	// Initialize AWS config with automatic credential detection
	// This will use AWS_PROFILE if set, or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY if available
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to load AWS config", "error", err)
		os.Exit(1)
	}

	// Create ELBv2 client for Target Group operations
	elbv2Client := elasticloadbalancingv2.NewFromConfig(awsCfg)

	m := manager.NewManager(cfg, k8sClient, elbv2Client, logger)

	if err := m.Start(ctx); err != nil {
		logger.ErrorContext(ctx, "Manager exited with error", "error", err)
		os.Exit(1)
	}
}
