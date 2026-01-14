package config

import (
	"os"
	"strconv"
)

const (
	defaultIntervalSeconds    = 60
	defaultLogLevel           = "info"
	defaultDryRun             = false
	defaultClientTimeout      = 10
	defaultMatchingLabelKey   = "app.kubernetes.io/managed-by"
	defaultMatchingLabelValue = "targetgroup-senpai"
)

type Config struct {
	LogLevel             string
	IntervalSeconds      int
	ClientTimeoutSeconds int
	DryRun               bool
	// MatchingLabelKey is the label key to match resources
	// e.g., `app.kubernetes.io/managed-by`
	MatchingLabelKey string
	// MatchingLabelValue is the label value to match resources
	// e.g., `targetgroup-senpai`
	MatchingLabelValue string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		IntervalSeconds:      defaultIntervalSeconds,
		LogLevel:             defaultLogLevel,
		DryRun:               defaultDryRun,
		ClientTimeoutSeconds: defaultClientTimeout,
		MatchingLabelKey:     defaultMatchingLabelKey,
		MatchingLabelValue:   defaultMatchingLabelValue,
	}

	// required

	// optional
	if v, ok := os.LookupEnv("MATCHING_LABEL_KEY"); ok {
		cfg.MatchingLabelKey = v
	}
	if v, ok := os.LookupEnv("MATCHING_LABEL_VALUE"); ok {
		cfg.MatchingLabelValue = v
	}
	if v, ok := os.LookupEnv("CLIENT_TIMEOUT_SECONDS"); ok {
		if secs, err := strconv.Atoi(v); err == nil {
			cfg.ClientTimeoutSeconds = secs
		}
	}
	if v, ok := os.LookupEnv("LOG_LEVEL"); ok {
		cfg.LogLevel = v
	}
	if v, ok := os.LookupEnv("INTERVAL_SECONDS"); ok {
		if secs, err := strconv.Atoi(v); err == nil {
			cfg.IntervalSeconds = secs
		}
	}
	if v, ok := os.LookupEnv("DRY_RUN"); ok {
		if dryRun, err := strconv.ParseBool(v); err == nil {
			cfg.DryRun = dryRun
		}
	}
	return cfg, nil
}
