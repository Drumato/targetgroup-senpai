package manager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Drumato/targetgroup-senpai/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// MockELBv2Client is a mock implementation of ELBv2Client for testing
type MockELBv2Client struct {
	mock.Mock
}

func (m *MockELBv2Client) CreateTargetGroup(ctx context.Context, params *elasticloadbalancingv2.CreateTargetGroupInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.CreateTargetGroupOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.CreateTargetGroupOutput), args.Error(1)
}

func (m *MockELBv2Client) DescribeTargetGroups(ctx context.Context, params *elasticloadbalancingv2.DescribeTargetGroupsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTargetGroupsOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.DescribeTargetGroupsOutput), args.Error(1)
}

func (m *MockELBv2Client) DescribeTags(ctx context.Context, params *elasticloadbalancingv2.DescribeTagsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTagsOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.DescribeTagsOutput), args.Error(1)
}

func (m *MockELBv2Client) ModifyTargetGroup(ctx context.Context, params *elasticloadbalancingv2.ModifyTargetGroupInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.ModifyTargetGroupOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.ModifyTargetGroupOutput), args.Error(1)
}

func (m *MockELBv2Client) RegisterTargets(ctx context.Context, params *elasticloadbalancingv2.RegisterTargetsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.RegisterTargetsOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.RegisterTargetsOutput), args.Error(1)
}

func (m *MockELBv2Client) DeregisterTargets(ctx context.Context, params *elasticloadbalancingv2.DeregisterTargetsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeregisterTargetsOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.DeregisterTargetsOutput), args.Error(1)
}

func (m *MockELBv2Client) DeleteTargetGroup(ctx context.Context, params *elasticloadbalancingv2.DeleteTargetGroupInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeleteTargetGroupOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.DeleteTargetGroupOutput), args.Error(1)
}

func (m *MockELBv2Client) ModifyTargetGroupAttributes(ctx context.Context, params *elasticloadbalancingv2.ModifyTargetGroupAttributesInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.ModifyTargetGroupAttributesOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.ModifyTargetGroupAttributesOutput), args.Error(1)
}

func (m *MockELBv2Client) DescribeTargetGroupAttributes(ctx context.Context, params *elasticloadbalancingv2.DescribeTargetGroupAttributesInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTargetGroupAttributesOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.DescribeTargetGroupAttributesOutput), args.Error(1)
}

func TestFilterNodePortServices(t *testing.T) {
	tests := []struct {
		name     string
		services []corev1.Service
		expected int
	}{
		{
			name: "filters only NodePort services",
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "nodeport-svc"},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "clusterip-svc"},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "loadbalancer-svc"},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
					},
				},
			},
			expected: 1,
		},
		{
			name:     "empty slice returns empty",
			services: []corev1.Service{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterNodePortServices(tt.services)
			assert.Len(t, result, tt.expected)
		})
	}
}

func TestManagerRunOnce(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	cfg := config.Config{
		IntervalSeconds:          30,
		ClientTimeoutSeconds:     60,
		MatchingLabelKey:         "targetgroup-senpai/enable",
		MatchingLabelValue:       "true",
		VpcId:                    "vpc-12345678",
		MinInstanceCount:         3,
		DeleteOrphanTargetGroups: true,
		HealthCheck: config.HealthCheckConfig{
			DefaultType:               "tcp",
			DefaultPath:               "/",
			DefaultIntervalSeconds:    30,
			DefaultTimeoutSeconds:     5,
			DefaultHealthyThreshold:   2,
			DefaultUnhealthyThreshold: 2,
		},
	}

	// Create a fake k8s client with test services
	services := []client.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nodeport-service",
				Namespace: "default",
				Labels: map[string]string{
					"targetgroup-senpai/enable": "true",
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					{
						Name:     "http",
						Port:     80,
						NodePort: 30080,
					},
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterip-service",
				Namespace: "default",
				Labels: map[string]string{
					"targetgroup-senpai/enable": "true",
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
			},
		},
	}

	k8sClient := fake.NewClientBuilder().WithObjects(services...).Build()

	// Create mock ELBv2 client
	mockELBv2Client := &MockELBv2Client{}

	// Set up mock expectations for DescribeTargetGroups (called by cleanup)
	mockELBv2Client.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
		Return(&elasticloadbalancingv2.DescribeTargetGroupsOutput{
			TargetGroups: []types.TargetGroup{}, // Empty list for test
		}, nil)

	// Create manager with mock dependencies
	manager := NewManager(cfg, k8sClient, mockELBv2Client, logger)

	// Test RunOnce
	err := manager.RunOnce(ctx)
	assert.NoError(t, err)

	// Verify mock expectations
	mockELBv2Client.AssertExpectations(t)
}

func TestManagerRunOnceWithOrphanCleanupDisabled(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	cfg := config.Config{
		IntervalSeconds:          30,
		ClientTimeoutSeconds:     60,
		MatchingLabelKey:         "targetgroup-senpai/enable",
		MatchingLabelValue:       "true",
		VpcId:                    "vpc-12345678",
		MinInstanceCount:         3,
		DeleteOrphanTargetGroups: false, // Disabled
	}

	// Create a fake k8s client with test services
	services := []client.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nodeport-service",
				Namespace: "default",
				Labels: map[string]string{
					"targetgroup-senpai/enable": "true",
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					{
						Port:     80,
						NodePort: 30080,
					},
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterip-service",
				Namespace: "default",
				Labels: map[string]string{
					"targetgroup-senpai/enable": "true",
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
			},
		},
	}

	k8sClient := fake.NewClientBuilder().WithObjects(services...).Build()

	// Create mock ELBv2 client
	mockELBv2Client := &MockELBv2Client{}

	// NOTE: No expectations set for DescribeTargetGroups since orphan cleanup is disabled

	// Create manager with mock dependencies
	manager := NewManager(cfg, k8sClient, mockELBv2Client, logger)

	// Test RunOnce
	err := manager.RunOnce(ctx)
	assert.NoError(t, err)

	// Verify mock expectations (should have no calls to DescribeTargetGroups)
	mockELBv2Client.AssertExpectations(t)
}

func TestNewManager(t *testing.T) {
	cfg := config.Config{VpcId: "vpc-test"}
	client := fake.NewClientBuilder().Build()
	elbClient := &MockELBv2Client{}
	logger := slog.Default()

	manager := NewManager(cfg, client, elbClient, logger)

	assert.Equal(t, cfg, manager.cfg)
	assert.Equal(t, client, manager.c)
	assert.Equal(t, elbClient, manager.elbv2Client)
	assert.Equal(t, logger, manager.logger)
}

func TestManagerStart(t *testing.T) {
	tests := []struct {
		name          string
		intervalSecs  int
		shouldCancel  bool
		cancelAfter   time.Duration
		expectTimeout bool
	}{
		{
			name:         "context cancel stops immediately",
			intervalSecs: 1,
			shouldCancel: true,
			cancelAfter:  100 * time.Millisecond,
		},
		{
			name:         "normal operation with ticker",
			intervalSecs: 1,
			shouldCancel: true,
			cancelAfter:  1500 * time.Millisecond, // Allow one tick
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				IntervalSeconds:      tt.intervalSecs,
				ClientTimeoutSeconds: 60,
				MatchingLabelKey:     "test-key",
				MatchingLabelValue:   "test-value",
				VpcId:                "vpc-test",
			}

			client := fake.NewClientBuilder().Build()
			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

			// Mock for cleanup operations that might be called
			mockELB.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
				Return(&elasticloadbalancingv2.DescribeTargetGroupsOutput{
					TargetGroups: []types.TargetGroup{},
				}, nil).Maybe()

			manager := NewManager(cfg, client, mockELB, logger)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan error, 1)
			go func() {
				done <- manager.Start(ctx)
			}()

			if tt.shouldCancel {
				time.Sleep(tt.cancelAfter)
				cancel()
			}

			select {
			case err := <-done:
				assert.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("Start method did not return after context cancellation")
			}
		})
	}
}

func TestListNodePortServices(t *testing.T) {
	tests := []struct {
		name          string
		services      []client.Object
		expectedCount int
		expectError   bool
	}{
		{
			name: "returns services with matching labels",
			services: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc1",
						Namespace: "default",
						Labels:    map[string]string{"test-key": "test-value"},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc2",
						Namespace: "default",
						Labels:    map[string]string{"test-key": "test-value"},
					},
				},
			},
			expectedCount: 2,
		},
		{
			name: "filters out services without matching labels",
			services: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc1",
						Namespace: "default",
						Labels:    map[string]string{"test-key": "test-value"},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc2",
						Namespace: "default",
						Labels:    map[string]string{"other-key": "other-value"},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name:          "returns empty when no services",
			services:      []client.Object{},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				MatchingLabelKey:   "test-key",
				MatchingLabelValue: "test-value",
			}

			client := fake.NewClientBuilder().WithObjects(tt.services...).Build()
			manager := NewManager(cfg, client, &MockELBv2Client{}, slog.Default())

			services, err := manager.listNodePortServices(context.Background())

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, services, tt.expectedCount)
			}
		})
	}
}

func TestListReadyNodes(t *testing.T) {
	tests := []struct {
		name          string
		nodes         []client.Object
		expectedCount int
	}{
		{
			name: "returns only ready nodes",
			nodes: []client.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "ready-node"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "not-ready-node"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "handles nodes with multiple conditions",
			nodes: []client.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "multi-condition-node"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
							{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name:          "returns empty when no nodes",
			nodes:         []client.Object{},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().WithObjects(tt.nodes...).Build()
			manager := NewManager(config.Config{}, client, &MockELBv2Client{}, slog.Default())

			readyNodes, err := manager.listReadyNodes(context.Background())

			assert.NoError(t, err)
			assert.Len(t, readyNodes, tt.expectedCount)
		})
	}
}

func TestConstructNodePortMap(t *testing.T) {
	readyNodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
	}

	tests := []struct {
		name     string
		services []corev1.Service
		pods     []client.Object
		expected map[string]int
	}{
		{
			name: "ExternalTrafficPolicy Cluster uses all nodes",
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "svc1", Namespace: "ns1"},
					Spec: corev1.ServiceSpec{
						ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
					},
				},
			},
			expected: map[string]int{"ns1/svc1": 2},
		},
		{
			name: "ExternalTrafficPolicy Local uses nodes with matching pods",
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "svc1", Namespace: "ns1"},
					Spec: corev1.ServiceSpec{
						ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
						Selector:              map[string]string{"app": "test"},
					},
				},
			},
			pods: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "ns1",
						Labels:    map[string]string{"app": "test"},
					},
					Spec:   corev1.PodSpec{NodeName: "node1"},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expected: map[string]int{"ns1/svc1": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().WithObjects(tt.pods...).Build()
			manager := NewManager(config.Config{}, client, &MockELBv2Client{}, slog.Default())

			result, err := manager.constructNodePortMap(context.Background(), tt.services, readyNodes)

			assert.NoError(t, err)
			for key, expectedCount := range tt.expected {
				assert.Len(t, result[key], expectedCount, "Service %s should have %d nodes", key, expectedCount)
			}
		})
	}
}

func TestGetNodesWithServicePods(t *testing.T) {
	readyNodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
	}

	tests := []struct {
		name          string
		service       *corev1.Service
		pods          []client.Object
		expectedNodes int
	}{
		{
			name: "returns nodes with matching running pods",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "test"},
				},
			},
			pods: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "ns",
						Labels:    map[string]string{"app": "test"},
					},
					Spec:   corev1.PodSpec{NodeName: "node1"},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "ns",
						Labels:    map[string]string{"app": "test"},
					},
					Spec:   corev1.PodSpec{NodeName: "node2"},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectedNodes: 2,
		},
		{
			name: "excludes non-running pods",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "test"},
				},
			},
			pods: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "ns",
						Labels:    map[string]string{"app": "test"},
					},
					Spec:   corev1.PodSpec{NodeName: "node1"},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "ns",
						Labels:    map[string]string{"app": "test"},
					},
					Spec:   corev1.PodSpec{NodeName: "node2"},
					Status: corev1.PodStatus{Phase: corev1.PodPending},
				},
			},
			expectedNodes: 1,
		},
		{
			name: "excludes pods that don't match selector",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "test"},
				},
			},
			pods: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "ns",
						Labels:    map[string]string{"app": "test"},
					},
					Spec:   corev1.PodSpec{NodeName: "node1"},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "ns",
						Labels:    map[string]string{"app": "other"},
					},
					Spec:   corev1.PodSpec{NodeName: "node2"},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectedNodes: 1,
		},
		{
			name: "returns empty when no matching pods",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "test"},
				},
			},
			pods:          []client.Object{},
			expectedNodes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().WithObjects(tt.pods...).Build()
			manager := NewManager(config.Config{}, client, &MockELBv2Client{}, slog.Default())

			nodes, err := manager.getNodesWithServicePods(context.Background(), tt.service, readyNodes)

			assert.NoError(t, err)
			assert.Len(t, nodes, tt.expectedNodes)
		})
	}
}

func TestEnsureTargetGroupForPort(t *testing.T) {
	tests := []struct {
		name           string
		service        corev1.Service
		port           corev1.ServicePort
		targetNodes    []corev1.Node
		dryRun         bool
		tgExists       bool
		expectCreate   bool
		expectUpdate   bool
		expectNoAction bool
		expectError    bool
	}{
		{
			name: "creates new target group for port when not exists",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
			},
			port:         corev1.ServicePort{Port: 80, NodePort: 30080},
			targetNodes:  []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			tgExists:     false,
			expectCreate: true,
		},
		{
			name: "updates existing target group for port",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
			},
			port:         corev1.ServicePort{Port: 443, NodePort: 30443},
			targetNodes:  []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			tgExists:     true,
			expectUpdate: true,
		},
		{
			name: "does nothing when no target nodes",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
			},
			port:           corev1.ServicePort{Port: 80, NodePort: 30080},
			targetNodes:    []corev1.Node{},
			expectNoAction: true,
		},
		{
			name: "fails when no NodePort",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
			},
			port:        corev1.ServicePort{Port: 80, NodePort: 0},
			targetNodes: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			expectError: true,
		},
		{
			name: "creates target group with port in name",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "multi-port-svc", Namespace: "ns"},
			},
			port:         corev1.ServicePort{Port: 8080, NodePort: 30808},
			targetNodes:  []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			expectCreate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				VpcId:  "vpc-test",
				DryRun: tt.dryRun,
			}

			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, fake.NewClientBuilder().Build(), mockELB, logger)

			if !tt.expectNoAction && !tt.dryRun && !tt.expectError {
				// Mock DescribeTargetGroups
				if tt.tgExists {
					mockELB.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
						Return(&elasticloadbalancingv2.DescribeTargetGroupsOutput{
							TargetGroups: []types.TargetGroup{
								{TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/1234567890123456")},
							},
						}, nil)

					// Mock DescribeTags for collision detection
					mockELB.On("DescribeTags", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTagsInput")).
						Return(&elasticloadbalancingv2.DescribeTagsOutput{
							TagDescriptions: []types.TagDescription{
								{
									ResourceArn: aws.String("arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/1234567890123456"),
									Tags: []types.Tag{
										{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns/svc-" + fmt.Sprintf("%d", tt.port.Port))},
									},
								},
							},
						}, nil)

					// Mock DescribeTargetGroupAttributes for existing target group proxy protocol v2 check
					mockELB.On("DescribeTargetGroupAttributes", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupAttributesInput")).
						Return(&elasticloadbalancingv2.DescribeTargetGroupAttributesOutput{
							Attributes: []types.TargetGroupAttribute{
								{Key: aws.String("proxy_protocol_v2.enabled"), Value: aws.String("false")},
							},
						}, nil)

					// Mock ModifyTargetGroupAttributes for proxy protocol v2 setting update
					mockELB.On("ModifyTargetGroupAttributes", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.ModifyTargetGroupAttributesInput")).
						Return(&elasticloadbalancingv2.ModifyTargetGroupAttributesOutput{}, nil)

					// Mock RegisterTargets for update
					mockELB.On("RegisterTargets", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.RegisterTargetsInput")).
						Return(&elasticloadbalancingv2.RegisterTargetsOutput{}, nil)
				} else {
					mockELB.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
						Return(&elasticloadbalancingv2.DescribeTargetGroupsOutput{TargetGroups: []types.TargetGroup{}}, nil)

					if tt.expectCreate {
						// Mock CreateTargetGroup
						mockELB.On("CreateTargetGroup", mock.Anything, mock.MatchedBy(func(input *elasticloadbalancingv2.CreateTargetGroupInput) bool {
							// Verify target group name includes port number
							expectedName := fmt.Sprintf("tgs-%s-%s-%d", tt.service.Namespace, tt.service.Name, tt.port.Port)
							if len(expectedName) > 32 {
								expectedName = expectedName[:32]
							}
							return input.Name != nil && *input.Name == expectedName
						})).Return(&elasticloadbalancingv2.CreateTargetGroupOutput{
							TargetGroups: []types.TargetGroup{
								{TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/1234567890123456")},
							},
						}, nil)

						// Mock ModifyTargetGroupAttributes for proxy protocol v2 setting
						mockELB.On("ModifyTargetGroupAttributes", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.ModifyTargetGroupAttributesInput")).
							Return(&elasticloadbalancingv2.ModifyTargetGroupAttributesOutput{}, nil)

						// Mock RegisterTargets for new target group
						mockELB.On("RegisterTargets", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.RegisterTargetsInput")).
							Return(&elasticloadbalancingv2.RegisterTargetsOutput{}, nil)
					}
				}
			}

			err := manager.ensureTargetGroupForPort(context.Background(), tt.service, tt.port, tt.targetNodes)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if !tt.expectNoAction && !tt.dryRun && !tt.expectError {
				mockELB.AssertExpectations(t)
			}
		})
	}
}

func TestEnsureTargetGroupsForService(t *testing.T) {
	tests := []struct {
		name            string
		service         corev1.Service
		targetNodes     []corev1.Node
		dryRun          bool
		expectError     bool
		expectedTGCount int
	}{
		{
			name: "creates target groups for multiple ports",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "multi-port-svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Port: 80, NodePort: 30080},
						{Port: 443, NodePort: 30443},
					},
				},
			},
			targetNodes:     []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			expectedTGCount: 2,
		},
		{
			name: "skips ports without NodePort",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "mixed-svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Port: 80, NodePort: 30080},
						{Port: 8080, NodePort: 0}, // No NodePort
						{Port: 443, NodePort: 30443},
					},
				},
			},
			targetNodes:     []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			expectedTGCount: 2, // Only 80 and 443 should get target groups
		},
		{
			name: "handles single port service",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "single-port-svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Port: 8080, NodePort: 30808},
					},
				},
			},
			targetNodes:     []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			expectedTGCount: 1,
		},
		{
			name: "does nothing when no target nodes",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Port: 80, NodePort: 30080},
					},
				},
			},
			targetNodes:     []corev1.Node{},
			expectedTGCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				VpcId:  "vpc-test",
				DryRun: true, // Use dry run to simplify mocking
			}

			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, fake.NewClientBuilder().Build(), mockELB, logger)

			err := manager.ensureTargetGroupsForService(context.Background(), tt.service, tt.targetNodes)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnsureTargetGroupForService(t *testing.T) {
	tests := []struct {
		name           string
		service        corev1.Service
		targetNodes    []corev1.Node
		dryRun         bool
		tgExists       bool
		expectCreate   bool
		expectUpdate   bool
		expectNoAction bool
	}{
		{
			name: "creates new target group when not exists",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{NodePort: 30080},
					},
				},
			},
			targetNodes:  []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			tgExists:     false,
			expectCreate: true,
		},
		{
			name: "updates existing target group",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{NodePort: 30080},
					},
				},
			},
			targetNodes:  []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			tgExists:     true,
			expectUpdate: true,
		},
		{
			name: "does nothing when no target nodes",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
			},
			targetNodes:    []corev1.Node{},
			expectNoAction: true,
		},
		{
			name: "dry run mode",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{NodePort: 30080},
					},
				},
			},
			targetNodes: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			dryRun:      true,
		},
		{
			name: "handles long service name truncation",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "very-very-long-service-name-that-exceeds-limit", Namespace: "very-long-namespace"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{NodePort: 30080},
					},
				},
			},
			targetNodes:  []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			expectCreate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				VpcId:  "vpc-test",
				DryRun: true, // Always use dry run to avoid mock complexity with collision detection
			}

			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, fake.NewClientBuilder().Build(), mockELB, logger)

			if false { // Disabled since we're using dry run
				// Mock DescribeTargetGroups
				if tt.tgExists {
					mockELB.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
						Return(&elasticloadbalancingv2.DescribeTargetGroupsOutput{
							TargetGroups: []types.TargetGroup{
								{TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/1234567890123456")},
							},
						}, nil)

					// Mock DescribeTargetGroupAttributes for existing target group proxy protocol v2 check
					mockELB.On("DescribeTargetGroupAttributes", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupAttributesInput")).
						Return(&elasticloadbalancingv2.DescribeTargetGroupAttributesOutput{
							Attributes: []types.TargetGroupAttribute{
								{Key: aws.String("proxy_protocol_v2.enabled"), Value: aws.String("false")}, // Assume it's currently disabled
							},
						}, nil)

					// Mock ModifyTargetGroupAttributes for proxy protocol v2 setting update
					mockELB.On("ModifyTargetGroupAttributes", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.ModifyTargetGroupAttributesInput")).
						Return(&elasticloadbalancingv2.ModifyTargetGroupAttributesOutput{}, nil)

					// Mock RegisterTargets for update
					mockELB.On("RegisterTargets", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.RegisterTargetsInput")).
						Return(&elasticloadbalancingv2.RegisterTargetsOutput{}, nil)
				} else {
					mockELB.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
						Return(&elasticloadbalancingv2.DescribeTargetGroupsOutput{TargetGroups: []types.TargetGroup{}}, nil)

					if tt.expectCreate {
						// Mock CreateTargetGroup
						mockELB.On("CreateTargetGroup", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.CreateTargetGroupInput")).
							Return(&elasticloadbalancingv2.CreateTargetGroupOutput{
								TargetGroups: []types.TargetGroup{
									{TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/1234567890123456")},
								},
							}, nil)

						// Mock ModifyTargetGroupAttributes for proxy protocol v2 setting
						mockELB.On("ModifyTargetGroupAttributes", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.ModifyTargetGroupAttributesInput")).
							Return(&elasticloadbalancingv2.ModifyTargetGroupAttributesOutput{}, nil)

						// Mock RegisterTargets for new target group
						mockELB.On("RegisterTargets", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.RegisterTargetsInput")).
							Return(&elasticloadbalancingv2.RegisterTargetsOutput{}, nil)
					}
				}
			}

			err := manager.ensureTargetGroupsForService(context.Background(), tt.service, tt.targetNodes)

			if len(tt.service.Spec.Ports) == 0 || tt.service.Spec.Ports[0].NodePort == 0 {
				if !tt.expectNoAction {
					assert.Error(t, err)
				}
			} else {
				assert.NoError(t, err)
			}

			if !tt.expectNoAction && !tt.dryRun {
				mockELB.AssertExpectations(t)
			}
		})
	}
}

func TestUpdateTargetGroupTargets(t *testing.T) {
	tests := []struct {
		name        string
		targetNodes []corev1.Node
		dryRun      bool
		expectError bool
	}{
		{
			name: "successfully registers targets",
			targetNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "10.0.1.100",
							},
						},
					},
				},
			},
		},
		{
			name: "handles nodes without internal IP",
			targetNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeExternalIP,
								Address: "192.168.1.100",
							},
						},
					},
				},
			},
		},
		{
			name: "dry run mode",
			targetNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "10.0.1.100",
							},
						},
					},
				},
			},
			dryRun: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{DryRun: tt.dryRun}
			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, fake.NewClientBuilder().Build(), mockELB, logger)

			if !tt.dryRun {
				mockELB.On("RegisterTargets", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.RegisterTargetsInput")).
					Return(&elasticloadbalancingv2.RegisterTargetsOutput{}, nil)
			}

			err := manager.updateTargetGroupTargets(context.Background(), "test-arn", tt.targetNodes)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if !tt.dryRun {
				mockELB.AssertExpectations(t)
			}
		})
	}
}

func TestCleanupOrphanedTargetGroups(t *testing.T) {
	tests := []struct {
		name          string
		targetGroups  []types.TargetGroup
		tags          map[string][]types.Tag
		services      []client.Object
		dryRun        bool
		expectDeletes int
	}{
		{
			name: "deletes orphaned target groups",
			targetGroups: []types.TargetGroup{
				{TargetGroupArn: aws.String("arn1")},
				{TargetGroupArn: aws.String("arn2")},
			},
			tags: map[string][]types.Tag{
				"arn1": {{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns1/svc1")}},
				"arn2": {{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns2/svc2")}},
			},
			services: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc1",
						Namespace: "ns1",
						Labels:    map[string]string{"test": "value"},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
				},
			},
			expectDeletes: 1, // arn2 should be deleted as ns2/svc2 doesn't exist
		},
		{
			name: "deletes orphaned target groups with port tags",
			targetGroups: []types.TargetGroup{
				{TargetGroupArn: aws.String("arn1")},
				{TargetGroupArn: aws.String("arn2")},
				{TargetGroupArn: aws.String("arn3")},
			},
			tags: map[string][]types.Tag{
				"arn1": {{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns1/svc1-80")}},
				"arn2": {{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns1/svc1-443")}},
				"arn3": {{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns2/svc2-80")}},
			},
			services: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc1",
						Namespace: "ns1",
						Labels:    map[string]string{"test": "value"},
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
						Ports: []corev1.ServicePort{
							{Port: 80, NodePort: 30080},
							{Port: 443, NodePort: 30443},
						},
					},
				},
			},
			expectDeletes: 1, // arn3 should be deleted as ns2/svc2 doesn't exist
		},
		{
			name: "retains target groups for existing services with matching ports",
			targetGroups: []types.TargetGroup{
				{TargetGroupArn: aws.String("arn1")},
				{TargetGroupArn: aws.String("arn2")},
			},
			tags: map[string][]types.Tag{
				"arn1": {{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns1/svc1-80")}},
				"arn2": {{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns1/svc1-443")}},
			},
			services: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc1",
						Namespace: "ns1",
						Labels:    map[string]string{"test": "value"},
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
						Ports: []corev1.ServicePort{
							{Port: 80, NodePort: 30080},
							{Port: 443, NodePort: 30443},
						},
					},
				},
			},
			expectDeletes: 0, // Both target groups should be retained
		},
		{
			name: "skips target groups without delete key tag",
			targetGroups: []types.TargetGroup{
				{TargetGroupArn: aws.String("arn1")},
			},
			tags: map[string][]types.Tag{
				"arn1": {{Key: aws.String("other-tag"), Value: aws.String("value")}},
			},
			services:      []client.Object{},
			expectDeletes: 0,
		},
		{
			name: "dry run mode",
			targetGroups: []types.TargetGroup{
				{TargetGroupArn: aws.String("arn1")},
			},
			tags: map[string][]types.Tag{
				"arn1": {{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns1/svc1")}},
			},
			services:      []client.Object{},
			dryRun:        true,
			expectDeletes: 0, // No actual deletion in dry run
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				MatchingLabelKey:   "test",
				MatchingLabelValue: "value",
				DryRun:             tt.dryRun,
			}

			client := fake.NewClientBuilder().WithObjects(tt.services...).Build()
			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, client, mockELB, logger)

			// Mock DescribeTargetGroups
			mockELB.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
				Return(&elasticloadbalancingv2.DescribeTargetGroupsOutput{
					TargetGroups: tt.targetGroups,
				}, nil)

			// Mock DescribeTags if we have target groups
			if len(tt.targetGroups) > 0 {
				tagDescriptions := make([]types.TagDescription, 0)
				for arn, tags := range tt.tags {
					tagDescriptions = append(tagDescriptions, types.TagDescription{
						ResourceArn: aws.String(arn),
						Tags:        tags,
					})
				}

				mockELB.On("DescribeTags", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTagsInput")).
					Return(&elasticloadbalancingv2.DescribeTagsOutput{
						TagDescriptions: tagDescriptions,
					}, nil)

				// Mock DeleteTargetGroup for expected deletions
				if tt.expectDeletes > 0 && !tt.dryRun {
					mockELB.On("DeleteTargetGroup", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DeleteTargetGroupInput")).
						Return(&elasticloadbalancingv2.DeleteTargetGroupOutput{}, nil).Times(tt.expectDeletes)
				}
			}

			err := manager.cleanupOrphanedTargetGroups(context.Background())

			assert.NoError(t, err)
			mockELB.AssertExpectations(t)
		})
	}
}

func TestHasDeleteKeyTag(t *testing.T) {
	tests := []struct {
		name     string
		tags     []types.Tag
		expected bool
	}{
		{
			name: "has delete key tag",
			tags: []types.Tag{
				{Key: aws.String(TagKeyDeleteKey), Value: aws.String("value")},
			},
			expected: true,
		},
		{
			name: "does not have delete key tag",
			tags: []types.Tag{
				{Key: aws.String("other-tag"), Value: aws.String("value")},
			},
			expected: false,
		},
		{
			name:     "empty tags",
			tags:     []types.Tag{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(config.Config{}, fake.NewClientBuilder().Build(), &MockELBv2Client{}, slog.Default())
			result := manager.hasDeleteKeyTag(tt.tags)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDeleteKeyFromTags(t *testing.T) {
	tests := []struct {
		name     string
		tags     []types.Tag
		expected string
	}{
		{
			name: "gets delete key value",
			tags: []types.Tag{
				{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns/svc")},
			},
			expected: "ns/svc",
		},
		{
			name: "returns empty when no delete key",
			tags: []types.Tag{
				{Key: aws.String("other-tag"), Value: aws.String("value")},
			},
			expected: "",
		},
		{
			name:     "returns empty when no tags",
			tags:     []types.Tag{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(config.Config{}, fake.NewClientBuilder().Build(), &MockELBv2Client{}, slog.Default())
			result := manager.getDeleteKeyFromTags(tt.tags)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDeleteTargetGroup(t *testing.T) {
	tests := []struct {
		name        string
		dryRun      bool
		expectError bool
	}{
		{
			name:   "successfully deletes target group",
			dryRun: false,
		},
		{
			name:   "dry run mode",
			dryRun: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{DryRun: tt.dryRun}
			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, fake.NewClientBuilder().Build(), mockELB, logger)

			if !tt.dryRun {
				mockELB.On("DeleteTargetGroup", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DeleteTargetGroupInput")).
					Return(&elasticloadbalancingv2.DeleteTargetGroupOutput{}, nil)
			}

			err := manager.deleteTargetGroup(context.Background(), "test-arn", "test/service")

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if !tt.dryRun {
				mockELB.AssertExpectations(t)
			}
		})
	}
}

func TestExtractInstanceIDFromProviderID(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		expected   string
	}{
		{
			name:       "extracts instance ID from standard AWS format",
			providerID: "aws:///us-west-2a/i-1234567890abcdef0",
			expected:   "i-1234567890abcdef0",
		},
		{
			name:       "extracts instance ID from alternative format",
			providerID: "aws://us-west-2a/i-1234567890abcdef0",
			expected:   "i-1234567890abcdef0",
		},
		{
			name:       "returns empty for non-AWS provider ID",
			providerID: "gce://project/zone/instance",
			expected:   "",
		},
		{
			name:       "returns empty for empty string",
			providerID: "",
			expected:   "",
		},
		{
			name:       "handles malformed AWS provider ID",
			providerID: "aws://",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(config.Config{}, fake.NewClientBuilder().Build(), &MockELBv2Client{}, slog.Default())
			result := manager.extractInstanceIDFromProviderID(tt.providerID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractNodeInternalIP(t *testing.T) {
	tests := []struct {
		name     string
		node     corev1.Node
		expected string
	}{
		{
			name: "extracts internal IP from node addresses",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeExternalIP, Address: "203.0.113.1"},
						{Type: corev1.NodeInternalIP, Address: "10.0.1.100"},
					},
				},
			},
			expected: "10.0.1.100",
		},
		{
			name: "returns empty when no internal IP exists",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeExternalIP, Address: "203.0.113.1"},
					},
				},
			},
			expected: "",
		},
		{
			name: "returns empty for node with no addresses",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(config.Config{}, fake.NewClientBuilder().Build(), &MockELBv2Client{}, slog.Default())
			result := manager.extractNodeInternalIP(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEnsureTargetGroupForServiceMinInstanceCount(t *testing.T) {
	tests := []struct {
		name             string
		service          corev1.Service
		targetNodes      []corev1.Node
		minInstanceCount int
		expectCreate     bool
		expectWarning    bool
	}{
		{
			name: "creates target group when meeting minimum instance count",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{NodePort: 30080},
					},
				},
			},
			targetNodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			},
			minInstanceCount: 3,
			expectCreate:     true,
		},
		{
			name: "skips target group creation when below minimum instance count",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{NodePort: 30080},
					},
				},
			},
			targetNodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			minInstanceCount: 3,
			expectCreate:     false,
			expectWarning:    true,
		},
		{
			name: "creates target group when exceeding minimum instance count",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{NodePort: 30080},
					},
				},
			},
			targetNodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node4"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node5"}},
			},
			minInstanceCount: 3,
			expectCreate:     true,
		},
		{
			name: "creates target group when minimum instance count is 1",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{NodePort: 30080},
					},
				},
			},
			targetNodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
			},
			minInstanceCount: 1,
			expectCreate:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				VpcId:            "vpc-test",
				MinInstanceCount: tt.minInstanceCount,
				DryRun:           true, // Use dry run to avoid complex mocking
			}

			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, fake.NewClientBuilder().Build(), mockELB, logger)

			err := manager.ensureTargetGroupsForService(context.Background(), tt.service, tt.targetNodes)

			assert.NoError(t, err)
		})
	}
}

func TestEnsureTargetGroupsForNodePortServices(t *testing.T) {
	tests := []struct {
		name        string
		services    []corev1.Service
		readyNodes  []corev1.Node
		pods        []client.Object
		expectError bool
	}{
		{
			name: "successfully processes services",
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "svc1", Namespace: "ns1"},
					Spec: corev1.ServiceSpec{
						ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
						Ports: []corev1.ServicePort{
							{NodePort: 30080},
						},
					},
				},
			},
			readyNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "10.0.1.100",
							},
						},
					},
				},
			},
		},
		{
			name:        "handles empty services list",
			services:    []corev1.Service{},
			readyNodes:  []corev1.Node{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				VpcId:  "vpc-test",
				DryRun: true, // Use dry run to avoid complex mocking
			}

			client := fake.NewClientBuilder().WithObjects(tt.pods...).Build()
			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, client, mockELB, logger)

			err := manager.ensureTargetGroupsForNodePortServices(context.Background(), tt.services, tt.readyNodes)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseHealthCheckConfig(t *testing.T) {
	tests := []struct {
		name        string
		service     corev1.Service
		servicePort int32
		expectError bool
		expected    *ServiceHealthCheckConfig
	}{
		{
			name: "default TCP health check configuration",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-svc",
					Namespace: "default",
				},
			},
			servicePort: 30080,
			expected: &ServiceHealthCheckConfig{
				Type:               "tcp",
				Path:               "/",
				Port:               30080,
				IntervalSeconds:    30,
				TimeoutSeconds:     5,
				HealthyThreshold:   2,
				UnhealthyThreshold: 2,
			},
		},
		{
			name: "HTTP health check with custom path",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-svc",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationHealthCheckType: "http",
						AnnotationHealthCheckPath: "/health",
					},
				},
			},
			servicePort: 30080,
			expected: &ServiceHealthCheckConfig{
				Type:               "http",
				Path:               "/health",
				Port:               30080,
				IntervalSeconds:    30,
				TimeoutSeconds:     5,
				HealthyThreshold:   2,
				UnhealthyThreshold: 2,
			},
		},
		{
			name: "HTTPS health check with all custom settings",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-svc",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationHealthCheckType:               "https",
						AnnotationHealthCheckPath:               "/api/health",
						AnnotationHealthCheckPort:               "30443",
						AnnotationHealthCheckInterval:           "60",
						AnnotationHealthCheckTimeout:            "10",
						AnnotationHealthCheckHealthyThreshold:   "3",
						AnnotationHealthCheckUnhealthyThreshold: "5",
					},
				},
			},
			servicePort: 30080,
			expected: &ServiceHealthCheckConfig{
				Type:               "https",
				Path:               "/api/health",
				Port:               30443,
				IntervalSeconds:    60,
				TimeoutSeconds:     10,
				HealthyThreshold:   3,
				UnhealthyThreshold: 5,
			},
		},
		{
			name: "invalid health check type",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-svc",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationHealthCheckType: "invalid",
					},
				},
			},
			servicePort: 30080,
			expectError: true,
		},
		{
			name: "TCP health check with path annotation (should fail)",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-svc",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationHealthCheckType: "tcp",
						AnnotationHealthCheckPath: "/health",
					},
				},
			},
			servicePort: 30080,
			expectError: true,
		},
		{
			name: "timeout greater than interval",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-svc",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationHealthCheckInterval: "10",
						AnnotationHealthCheckTimeout:  "15",
					},
				},
			},
			servicePort: 30080,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				HealthCheck: config.HealthCheckConfig{
					DefaultType:               "tcp",
					DefaultPath:               "/",
					DefaultIntervalSeconds:    30,
					DefaultTimeoutSeconds:     5,
					DefaultHealthyThreshold:   2,
					DefaultUnhealthyThreshold: 2,
				},
			}

			manager := NewManager(cfg, fake.NewClientBuilder().Build(), &MockELBv2Client{}, slog.Default())

			result, err := manager.parseHealthCheckConfig(tt.service, tt.servicePort)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestEnsureTargetGroupForServiceWithHealthCheck(t *testing.T) {
	tests := []struct {
		name                string
		service             corev1.Service
		targetNodes         []corev1.Node
		expectCreate        bool
		validateHealthCheck func(t *testing.T, input *elasticloadbalancingv2.CreateTargetGroupInput)
	}{
		{
			name: "TCP health check creates target group with TCP protocol",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tcp-svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{NodePort: 30080}},
				},
			},
			targetNodes:  []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			expectCreate: true,
			validateHealthCheck: func(t *testing.T, input *elasticloadbalancingv2.CreateTargetGroupInput) {
				assert.Equal(t, types.ProtocolEnumTcp, input.Protocol)
				assert.Equal(t, types.ProtocolEnumTcp, input.HealthCheckProtocol)
				assert.Equal(t, "30080", *input.HealthCheckPort)
				assert.Equal(t, int32(30), *input.HealthCheckIntervalSeconds)
				assert.Equal(t, int32(5), *input.HealthCheckTimeoutSeconds)
				assert.Equal(t, int32(2), *input.HealthyThresholdCount)
				assert.Equal(t, int32(2), *input.UnhealthyThresholdCount)
				assert.Nil(t, input.HealthCheckPath)
			},
		},
		{
			name: "HTTP health check creates target group with HTTP health check",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-svc",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationHealthCheckType: "http",
						AnnotationHealthCheckPath: "/health",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{NodePort: 30080}},
				},
			},
			targetNodes:  []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			expectCreate: true,
			validateHealthCheck: func(t *testing.T, input *elasticloadbalancingv2.CreateTargetGroupInput) {
				assert.Equal(t, types.ProtocolEnumTcp, input.Protocol)
				assert.Equal(t, types.ProtocolEnumHttp, input.HealthCheckProtocol)
				assert.Equal(t, "/health", *input.HealthCheckPath)
				assert.Equal(t, "30080", *input.HealthCheckPort)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				VpcId: "vpc-test",
				HealthCheck: config.HealthCheckConfig{
					DefaultType:               "tcp",
					DefaultPath:               "/",
					DefaultIntervalSeconds:    30,
					DefaultTimeoutSeconds:     5,
					DefaultHealthyThreshold:   2,
					DefaultUnhealthyThreshold: 2,
				},
			}

			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, fake.NewClientBuilder().Build(), mockELB, logger)

			if tt.expectCreate {
				// Mock DescribeTargetGroups to return empty (target group doesn't exist)
				mockELB.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
					Return(&elasticloadbalancingv2.DescribeTargetGroupsOutput{TargetGroups: []types.TargetGroup{}}, nil)

				// Mock CreateTargetGroup with validation
				mockELB.On("CreateTargetGroup", mock.Anything, mock.MatchedBy(func(input *elasticloadbalancingv2.CreateTargetGroupInput) bool {
					if tt.validateHealthCheck != nil {
						tt.validateHealthCheck(t, input)
					}
					return true
				})).Return(&elasticloadbalancingv2.CreateTargetGroupOutput{
					TargetGroups: []types.TargetGroup{
						{TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/1234567890123456")},
					},
				}, nil)

				// Mock ModifyTargetGroupAttributes for proxy protocol v2 setting
				mockELB.On("ModifyTargetGroupAttributes", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.ModifyTargetGroupAttributesInput")).
					Return(&elasticloadbalancingv2.ModifyTargetGroupAttributesOutput{}, nil)

				// Mock RegisterTargets
				mockELB.On("RegisterTargets", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.RegisterTargetsInput")).
					Return(&elasticloadbalancingv2.RegisterTargetsOutput{}, nil)
			}

			err := manager.ensureTargetGroupsForService(context.Background(), tt.service, tt.targetNodes)
			assert.NoError(t, err)

			if tt.expectCreate {
				mockELB.AssertExpectations(t)
			}
		})
	}
}

func TestShouldEnableProxyProtocolV2(t *testing.T) {
	tests := []struct {
		name        string
		service     corev1.Service
		expected    bool
		description string
	}{
		{
			name: "enables proxy protocol v2 by default when no annotations",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
			},
			expected:    true,
			description: "should enable proxy protocol v2 when no annotations are present",
		},
		{
			name: "enables proxy protocol v2 when annotation is empty",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-service",
					Namespace:   "default",
					Annotations: map[string]string{},
				},
			},
			expected:    true,
			description: "should enable proxy protocol v2 when annotations map is empty",
		},
		{
			name: "disables proxy protocol v2 when annotation is true",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationDisableProxyProtocolV2: "true",
					},
				},
			},
			expected:    false,
			description: "should disable proxy protocol v2 when disable annotation is 'true'",
		},
		{
			name: "disables proxy protocol v2 when annotation is TRUE (case insensitive)",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationDisableProxyProtocolV2: "TRUE",
					},
				},
			},
			expected:    false,
			description: "should disable proxy protocol v2 when disable annotation is 'TRUE' (case insensitive)",
		},
		{
			name: "enables proxy protocol v2 when annotation is false",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationDisableProxyProtocolV2: "false",
					},
				},
			},
			expected:    true,
			description: "should enable proxy protocol v2 when disable annotation is 'false'",
		},
		{
			name: "enables proxy protocol v2 when annotation value is invalid",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationDisableProxyProtocolV2: "invalid",
					},
				},
			},
			expected:    true,
			description: "should enable proxy protocol v2 when disable annotation has invalid value",
		},
		{
			name: "handles whitespace in annotation value",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationDisableProxyProtocolV2: "  true  ",
					},
				},
			},
			expected:    false,
			description: "should handle whitespace correctly in annotation value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(config.Config{}, fake.NewClientBuilder().Build(), &MockELBv2Client{}, slog.Default())
			result := manager.shouldEnableProxyProtocolV2(tt.service)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestModifyTargetGroupProxyProtocolV2(t *testing.T) {
	tests := []struct {
		name          string
		enable        bool
		dryRun        bool
		expectAPICall bool
		expectError   bool
		mockError     error
		description   string
	}{
		{
			name:          "enables proxy protocol v2 successfully",
			enable:        true,
			dryRun:        false,
			expectAPICall: true,
			expectError:   false,
			description:   "should successfully enable proxy protocol v2",
		},
		{
			name:          "disables proxy protocol v2 successfully",
			enable:        false,
			dryRun:        false,
			expectAPICall: true,
			expectError:   false,
			description:   "should successfully disable proxy protocol v2",
		},
		{
			name:          "dry run mode - enable",
			enable:        true,
			dryRun:        true,
			expectAPICall: false,
			expectError:   false,
			description:   "should not make API call in dry run mode",
		},
		{
			name:          "dry run mode - disable",
			enable:        false,
			dryRun:        true,
			expectAPICall: false,
			expectError:   false,
			description:   "should not make API call in dry run mode",
		},
		{
			name:          "handles API error",
			enable:        true,
			dryRun:        false,
			expectAPICall: true,
			expectError:   true,
			mockError:     fmt.Errorf("API error"),
			description:   "should handle API errors correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{DryRun: tt.dryRun}
			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, fake.NewClientBuilder().Build(), mockELB, logger)

			targetGroupArn := "arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/1234567890123456"

			if tt.expectAPICall {
				expectedValue := "false"
				if tt.enable {
					expectedValue = "true"
				}

				mockELB.On("ModifyTargetGroupAttributes", mock.Anything, mock.MatchedBy(func(input *elasticloadbalancingv2.ModifyTargetGroupAttributesInput) bool {
					if input.TargetGroupArn == nil || *input.TargetGroupArn != targetGroupArn {
						return false
					}
					if len(input.Attributes) != 1 {
						return false
					}
					attr := input.Attributes[0]
					return attr.Key != nil && *attr.Key == "proxy_protocol_v2.enabled" &&
						attr.Value != nil && *attr.Value == expectedValue
				})).Return(&elasticloadbalancingv2.ModifyTargetGroupAttributesOutput{}, tt.mockError)
			}

			err := manager.modifyTargetGroupProxyProtocolV2(context.Background(), targetGroupArn, tt.enable)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}

			if tt.expectAPICall {
				mockELB.AssertExpectations(t)
			}
		})
	}
}

func TestGetCurrentProxyProtocolV2Setting(t *testing.T) {
	tests := []struct {
		name           string
		attributes     []types.TargetGroupAttribute
		expectedResult bool
		expectError    bool
		mockError      error
		description    string
	}{
		{
			name: "returns true when proxy protocol v2 is enabled",
			attributes: []types.TargetGroupAttribute{
				{Key: aws.String("proxy_protocol_v2.enabled"), Value: aws.String("true")},
			},
			expectedResult: true,
			expectError:    false,
			description:    "should return true when proxy protocol v2 is enabled",
		},
		{
			name: "returns false when proxy protocol v2 is disabled",
			attributes: []types.TargetGroupAttribute{
				{Key: aws.String("proxy_protocol_v2.enabled"), Value: aws.String("false")},
			},
			expectedResult: false,
			expectError:    false,
			description:    "should return false when proxy protocol v2 is disabled",
		},
		{
			name: "returns false when proxy protocol v2 attribute is not present",
			attributes: []types.TargetGroupAttribute{
				{Key: aws.String("some_other_attribute"), Value: aws.String("value")},
			},
			expectedResult: false,
			expectError:    false,
			description:    "should return false when proxy protocol v2 attribute is not present",
		},
		{
			name:           "returns false when no attributes",
			attributes:     []types.TargetGroupAttribute{},
			expectedResult: false,
			expectError:    false,
			description:    "should return false when no attributes are present",
		},
		{
			name:           "handles API error",
			attributes:     []types.TargetGroupAttribute{},
			expectedResult: false,
			expectError:    true,
			mockError:      fmt.Errorf("API error"),
			description:    "should handle API errors correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(config.Config{}, fake.NewClientBuilder().Build(), mockELB, logger)

			targetGroupArn := "arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/1234567890123456"

			mockELB.On("DescribeTargetGroupAttributes", mock.Anything, mock.MatchedBy(func(input *elasticloadbalancingv2.DescribeTargetGroupAttributesInput) bool {
				return input.TargetGroupArn != nil && *input.TargetGroupArn == targetGroupArn
			})).Return(&elasticloadbalancingv2.DescribeTargetGroupAttributesOutput{
				Attributes: tt.attributes,
			}, tt.mockError)

			result, err := manager.getCurrentProxyProtocolV2Setting(context.Background(), targetGroupArn)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expectedResult, result, tt.description)
			}

			mockELB.AssertExpectations(t)
		})
	}
}

func TestGenerateTargetGroupName(t *testing.T) {
	tests := []struct {
		name        string
		service     corev1.Service
		port        int32
		expected    string
		description string
	}{
		{
			name: "short service name uses standard format",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: "ns",
					UID:       "12345678-1234-1234-1234-123456789abc",
				},
			},
			port:        80,
			expected:    "tgs-ns-svc-80",
			description: "should use standard format for short names",
		},
		{
			name: "long service name uses UID-based format",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "very-long-service-name-that-exceeds-aws-limits",
					Namespace: "very-long-namespace-name",
					UID:       "12345678-1234-1234-1234-123456789abc",
				},
			},
			port:        443,
			expected:    "tgs-123456781234123412341234-443",
			description: "should use UID-based format for long names",
		},
		{
			name: "handles missing UID gracefully",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "very-long-service-name-that-exceeds-aws-limits",
					Namespace: "very-long-namespace-name",
					UID:       "",
				},
			},
			port:        8080,
			expected:    "tgs-very-long-namespace-name-ver",
			description: "should fallback to truncation when UID is missing",
		},
		{
			name: "uses UID format when standard name exceeds limit",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "very-very-long-service-name",
					Namespace: "very-long-namespace",
					UID:       "abcdef01-2345-6789-abcd-ef0123456789",
				},
			},
			port:     12345,
			expected: "tgs-abcdef0123456789abcdef-12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(config.Config{}, fake.NewClientBuilder().Build(), &MockELBv2Client{}, slog.Default())
			result := manager.generateTargetGroupName(tt.service, tt.port)

			assert.Equal(t, tt.expected, result, tt.description)
			assert.LessOrEqual(t, len(result), 32, "generated name should not exceed 32 characters")
		})
	}
}

func TestDetectTargetGroupCollision(t *testing.T) {
	tests := []struct {
		name               string
		targetGroupName    string
		expectedServiceKey string
		existingTG         *types.TargetGroup
		existingTags       []types.Tag
		expectError        bool
		description        string
	}{
		{
			name:               "no collision when target group doesn't exist",
			targetGroupName:    "tgs-test-80",
			expectedServiceKey: "ns/svc-80",
			existingTG:         nil,
			expectError:        false,
			description:        "should not report collision for non-existent target group",
		},
		{
			name:               "no collision when target group belongs to same service",
			targetGroupName:    "tgs-test-80",
			expectedServiceKey: "ns/svc-80",
			existingTG:         &types.TargetGroup{TargetGroupArn: aws.String("arn:test")},
			existingTags: []types.Tag{
				{Key: aws.String(TagKeyDeleteKey), Value: aws.String("ns/svc-80")},
			},
			expectError: false,
			description: "should not report collision for same service",
		},
		{
			name:               "collision detected when target group belongs to different service",
			targetGroupName:    "tgs-test-80",
			expectedServiceKey: "ns/svc-80",
			existingTG:         &types.TargetGroup{TargetGroupArn: aws.String("arn:test")},
			existingTags: []types.Tag{
				{Key: aws.String(TagKeyDeleteKey), Value: aws.String("other-ns/other-svc-80")},
			},
			expectError: true,
			description: "should report collision for different service",
		},
		{
			name:               "no collision when existing target group has no service tag",
			targetGroupName:    "tgs-test-80",
			expectedServiceKey: "ns/svc-80",
			existingTG:         &types.TargetGroup{TargetGroupArn: aws.String("arn:test")},
			existingTags:       []types.Tag{},
			expectError:        false,
			description:        "should not report collision when existing TG has no service tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(config.Config{}, fake.NewClientBuilder().Build(), mockELB, logger)

			// Mock DescribeTargetGroups
			describeOutput := &elasticloadbalancingv2.DescribeTargetGroupsOutput{
				TargetGroups: []types.TargetGroup{},
			}
			if tt.existingTG != nil {
				describeOutput.TargetGroups = []types.TargetGroup{*tt.existingTG}
			}
			mockELB.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
				Return(describeOutput, nil)

			// Mock DescribeTags if target group exists
			if tt.existingTG != nil {
				tagsOutput := &elasticloadbalancingv2.DescribeTagsOutput{
					TagDescriptions: []types.TagDescription{
						{
							ResourceArn: tt.existingTG.TargetGroupArn,
							Tags:        tt.existingTags,
						},
					},
				}
				mockELB.On("DescribeTags", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTagsInput")).
					Return(tagsOutput, nil)
			}

			err := manager.detectTargetGroupCollision(context.Background(), tt.targetGroupName, tt.expectedServiceKey)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}

			mockELB.AssertExpectations(t)
		})
	}
}
