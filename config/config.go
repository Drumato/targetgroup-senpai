package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/samber/lo"
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
	// VpcId is the VPC ID where target groups will be created
	VpcId string
}

type configField struct {
	envKey       string
	value        interface{}
	defaultValue interface{}
	optional     bool
}

func loadConfigFieldFromEnv(err error, pair configField, _ int) error {
	// propagate previous error
	if err != nil {
		return err
	}

	envValue, exists := os.LookupEnv(pair.envKey)
	if !exists && !pair.optional {
		return fmt.Errorf("environment variable %s is required", pair.envKey)
	}

	switch v := pair.value.(type) {
	case *int:
		parsed, err := strconv.Atoi(envValue)
		if err != nil {
			return err
		}
		*v = parsed
	case *string:
		*v = envValue
	case *bool:
		parsed, err := strconv.ParseBool(envValue)
		if err != nil {
			return err
		}
		*v = parsed
	}

	return nil
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

	pairs := []configField{
		{"INTERVAL_SECONDS", &cfg.IntervalSeconds, defaultIntervalSeconds, true},
		{"LOG_LEVEL", &cfg.LogLevel, defaultLogLevel, true},
		{"DRY_RUN", &cfg.DryRun, defaultDryRun, true},
		{"CLIENT_TIMEOUT_SECONDS", &cfg.ClientTimeoutSeconds, defaultClientTimeout, true},
		{"MATCHING_LABEL_KEY", &cfg.MatchingLabelKey, defaultMatchingLabelKey, true},
		{"MATCHING_LABEL_VALUE", &cfg.MatchingLabelValue, defaultMatchingLabelValue, true},
		{"VPC_ID", &cfg.VpcId, "", false},
	}

	if err := lo.Reduce(pairs, loadConfigFieldFromEnv, nil); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
