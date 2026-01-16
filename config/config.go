package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/samber/lo"
)

const (
	defaultIntervalSeconds          = 60
	defaultLogLevel                 = "info"
	defaultDryRun                   = false
	defaultClientTimeout            = 10
	defaultMatchingLabelKey         = "app.kubernetes.io/managed-by"
	defaultMatchingLabelValue       = "targetgroup-senpai"
	defaultMinInstanceCount         = 3
	defaultDeleteOrphanTargetGroups = true

	// Health Check Defaults
	defaultHealthCheckType               = "tcp"
	defaultHealthCheckPath               = "/"
	defaultHealthCheckIntervalSeconds    = 30
	defaultHealthCheckTimeoutSeconds     = 5
	defaultHealthCheckHealthyThreshold   = 2
	defaultHealthCheckUnhealthyThreshold = 2
)

type HealthCheckConfig struct {
	DefaultType               string
	DefaultPath               string
	DefaultIntervalSeconds    int32
	DefaultTimeoutSeconds     int32
	DefaultHealthyThreshold   int32
	DefaultUnhealthyThreshold int32
}

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
	// MinInstanceCount is the minimum number of instances required for target group operations
	MinInstanceCount int
	// DeleteOrphanTargetGroups controls whether to delete target groups without corresponding services
	DeleteOrphanTargetGroups bool
	// HealthCheck contains default health check configuration
	HealthCheck HealthCheckConfig
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

	// If the environment variable doesn't exist, use default value
	if !exists {
		return nil
	}

	switch v := pair.value.(type) {
	case *int:
		// If empty string and optional, use default value
		if envValue == "" && pair.optional {
			if defaultVal, ok := pair.defaultValue.(int); ok {
				*v = defaultVal
			}
			return nil
		}
		parsed, err := strconv.Atoi(envValue)
		if err != nil {
			return err
		}
		*v = parsed
	case *string:
		*v = envValue
	case *bool:
		// If empty string and optional, use default value
		if envValue == "" && pair.optional {
			if defaultVal, ok := pair.defaultValue.(bool); ok {
				*v = defaultVal
			}
			return nil
		}
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
		IntervalSeconds:          defaultIntervalSeconds,
		LogLevel:                 defaultLogLevel,
		DryRun:                   defaultDryRun,
		ClientTimeoutSeconds:     defaultClientTimeout,
		MatchingLabelKey:         defaultMatchingLabelKey,
		MatchingLabelValue:       defaultMatchingLabelValue,
		MinInstanceCount:         defaultMinInstanceCount,
		DeleteOrphanTargetGroups: defaultDeleteOrphanTargetGroups,
		HealthCheck: HealthCheckConfig{
			DefaultType:               defaultHealthCheckType,
			DefaultPath:               defaultHealthCheckPath,
			DefaultIntervalSeconds:    defaultHealthCheckIntervalSeconds,
			DefaultTimeoutSeconds:     defaultHealthCheckTimeoutSeconds,
			DefaultHealthyThreshold:   defaultHealthCheckHealthyThreshold,
			DefaultUnhealthyThreshold: defaultHealthCheckUnhealthyThreshold,
		},
	}

	pairs := []configField{
		{"INTERVAL_SECONDS", &cfg.IntervalSeconds, defaultIntervalSeconds, true},
		{"LOG_LEVEL", &cfg.LogLevel, defaultLogLevel, true},
		{"DRY_RUN", &cfg.DryRun, defaultDryRun, true},
		{"CLIENT_TIMEOUT_SECONDS", &cfg.ClientTimeoutSeconds, defaultClientTimeout, true},
		{"MATCHING_LABEL_KEY", &cfg.MatchingLabelKey, defaultMatchingLabelKey, true},
		{"MATCHING_LABEL_VALUE", &cfg.MatchingLabelValue, defaultMatchingLabelValue, true},
		{"MIN_INSTANCE_COUNT", &cfg.MinInstanceCount, defaultMinInstanceCount, true},
		{"DELETE_ORPHAN_TARGET_GROUPS", &cfg.DeleteOrphanTargetGroups, defaultDeleteOrphanTargetGroups, true},
		{"VPC_ID", &cfg.VpcId, "", false},
	}

	if err := lo.Reduce(pairs, loadConfigFieldFromEnv, nil); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
