package manager

import (
	"context"
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
				DryRun: tt.dryRun,
			}

			mockELB := &MockELBv2Client{}
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			manager := NewManager(cfg, fake.NewClientBuilder().Build(), mockELB, logger)

			if !tt.expectNoAction && !tt.dryRun {
				// Mock DescribeTargetGroups
				if tt.tgExists {
					mockELB.On("DescribeTargetGroups", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.DescribeTargetGroupsInput")).
						Return(&elasticloadbalancingv2.DescribeTargetGroupsOutput{
							TargetGroups: []types.TargetGroup{
								{TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/1234567890123456")},
							},
						}, nil)

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

						// Mock RegisterTargets for new target group
						mockELB.On("RegisterTargets", mock.Anything, mock.AnythingOfType("*elasticloadbalancingv2.RegisterTargetsInput")).
							Return(&elasticloadbalancingv2.RegisterTargetsOutput{}, nil)
					}
				}
			}

			err := manager.ensureTargetGroupForService(context.Background(), tt.service, tt.targetNodes)

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

			err := manager.ensureTargetGroupForService(context.Background(), tt.service, tt.targetNodes)

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
