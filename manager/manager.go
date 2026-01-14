package manager

import (
	"context"
	"log/slog"
	"time"

	"github.com/Drumato/targetgroup-senpai/config"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Manager struct {
	cfg    config.Config
	c      client.Client
	logger *slog.Logger
}

func NewManager(
	cfg config.Config,
	c client.Client,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		cfg:    cfg,
		c:      c,
		logger: logger,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	ticker := time.NewTicker(time.Duration(m.cfg.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.RunOnce(ctx); err != nil {
				m.logger.ErrorContext(ctx, "failed to RunOnce", "error", err)
				continue
			}
		}
	}
}

func (m *Manager) RunOnce(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.cfg.ClientTimeoutSeconds)*time.Second)
	defer cancel()

	serviceList := corev1.ServiceList{}

	listOptions := []client.ListOption{
		client.MatchingLabels{m.cfg.MatchingLabelKey: m.cfg.MatchingLabelValue},
	}
	if err := m.c.List(ctx, &serviceList, listOptions...); err != nil {
		return err
	}
	m.logger.InfoContext(ctx, "found senpai's target services", "count", len(serviceList.Items))

	for _, svc := range serviceList.Items {
		if svc.Spec.Type != corev1.ServiceTypeNodePort {
			m.logger.InfoContext(ctx, "skipping non-NodePort service", "service", svc.Name, "namespace", svc.Namespace, "type", svc.Spec.Type)
			continue
		}

		if m.cfg.DryRun {
			m.logger.InfoContext(ctx, "dry run: would process service", "service", svc.Name, "namespace", svc.Namespace)
		}
	}
	return nil
}
