package manager

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Drumato/targetgroup-senpai/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// TagKeyDeleteKey is the key for the tag that indicates which service this target group belongs to
	TagKeyDeleteKey = "targetgroup-senpai-delete-key"
)

// ELBv2Client defines the interface for ELBv2 operations needed by the manager
type ELBv2Client interface {
	CreateTargetGroup(ctx context.Context, params *elasticloadbalancingv2.CreateTargetGroupInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.CreateTargetGroupOutput, error)
	DescribeTargetGroups(ctx context.Context, params *elasticloadbalancingv2.DescribeTargetGroupsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTargetGroupsOutput, error)
	DescribeTags(ctx context.Context, params *elasticloadbalancingv2.DescribeTagsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTagsOutput, error)
	ModifyTargetGroup(ctx context.Context, params *elasticloadbalancingv2.ModifyTargetGroupInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.ModifyTargetGroupOutput, error)
	RegisterTargets(ctx context.Context, params *elasticloadbalancingv2.RegisterTargetsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.RegisterTargetsOutput, error)
	DeregisterTargets(ctx context.Context, params *elasticloadbalancingv2.DeregisterTargetsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeregisterTargetsOutput, error)
	DeleteTargetGroup(ctx context.Context, params *elasticloadbalancingv2.DeleteTargetGroupInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DeleteTargetGroupOutput, error)
}

type Manager struct {
	cfg         config.Config
	c           client.Client
	elbv2Client ELBv2Client
	logger      *slog.Logger
}

func NewManager(
	cfg config.Config, c client.Client,
	elbv2Client ELBv2Client,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		cfg:         cfg,
		c:           c,
		elbv2Client: elbv2Client,
		logger:      logger,
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

	// Get NodePort services
	serviceList, err := m.listNodePortServices(ctx)
	if err != nil {
		return err
	}
	m.logger.InfoContext(ctx, "found senpai's target services", "count", len(serviceList))

	// Get ready nodes
	readyNodes, err := m.listReadyNodes(ctx)
	if err != nil {
		return err
	}
	m.logger.InfoContext(ctx, "found available cluster nodes", "count", len(readyNodes))

	// Filter to only NodePort services
	nodePortServices := filterNodePortServices(serviceList)
	m.logger.InfoContext(ctx, "found NodePort services", "count", len(nodePortServices))

	// Ensure target groups for NodePort services
	if err := m.ensureTargetGroupsForNodePortServices(ctx, nodePortServices, readyNodes); err != nil {
		m.logger.ErrorContext(ctx, "failed to ensure target groups", "error", err)
		return err
	}

	// Cleanup orphaned target groups (only if enabled)
	if m.cfg.DeleteOrphanTargetGroups {
		if err := m.cleanupOrphanedTargetGroups(ctx); err != nil {
			m.logger.ErrorContext(ctx, "failed to cleanup orphaned target groups", "error", err)
			return err
		}
	} else {
		m.logger.InfoContext(ctx, "Orphan target group cleanup is disabled")
	}

	return nil
}

func (m *Manager) listNodePortServices(ctx context.Context) ([]corev1.Service, error) {
	serviceList := corev1.ServiceList{}

	listOptions := []client.ListOption{
		client.MatchingLabels{m.cfg.MatchingLabelKey: m.cfg.MatchingLabelValue},
	}
	if err := m.c.List(ctx, &serviceList, listOptions...); err != nil {
		return nil, err
	}

	return serviceList.Items, nil
}

// <namespace>/<service name>: <nodes>
type NodePortTargetNodes map[string][]corev1.Node

func (m *Manager) listReadyNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList := corev1.NodeList{}

	if err := m.c.List(ctx, &nodeList); err != nil {
		return nil, err
	}

	return lo.Filter(nodeList.Items, func(node corev1.Node, _ int) bool {
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				return true
			}
		}

		return false
	}), nil
}

func (m *Manager) constructNodePortMap(
	ctx context.Context,
	services []corev1.Service,
	readyNodes []corev1.Node,
) (NodePortTargetNodes, error) {
	result := make(NodePortTargetNodes)

	for _, svc := range services {
		serviceKey := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)

		if svc.Spec.ExternalTrafficPolicy != corev1.ServiceExternalTrafficPolicyTypeLocal {
			// If ExternalTrafficPolicy is not Local, all ready nodes can be targets
			result[serviceKey] = readyNodes
			continue
		}

		// For ExternalTrafficPolicy=Local, find nodes that have pods matching this service
		targetNodes, err := m.getNodesWithServicePods(ctx, &svc, readyNodes)
		if err != nil {
			return nil, fmt.Errorf("failed to get nodes with service pods for %s: %w", serviceKey, err)
		}

		result[serviceKey] = targetNodes
	}

	return result, nil
}

// getNodesWithServicePods returns nodes that have pods matching the given service
func (m *Manager) getNodesWithServicePods(ctx context.Context, svc *corev1.Service, readyNodes []corev1.Node) ([]corev1.Node, error) {
	// Get all pods in the service's namespace
	podList := corev1.PodList{}
	listOptions := []client.ListOption{
		client.InNamespace(svc.Namespace),
	}

	if err := m.c.List(ctx, &podList, listOptions...); err != nil {
		return nil, err
	}

	// Find pods that match the service selector
	servicePods := lo.Filter(podList.Items, func(pod corev1.Pod, _ int) bool {
		// Skip pods that are not running or ready
		if pod.Status.Phase != corev1.PodRunning {
			return false
		}

		// Check if pod matches service selector
		for key, value := range svc.Spec.Selector {
			if podValue, exists := pod.Labels[key]; !exists || podValue != value {
				return false
			}
		}
		return true
	})

	// Get the node names where these pods are running
	podNodeNames := lo.Map(servicePods, func(pod corev1.Pod, _ int) string {
		return pod.Spec.NodeName
	})

	// Filter ready nodes to only include those with matching pods
	targetNodes := lo.Filter(readyNodes, func(node corev1.Node, _ int) bool {
		return lo.Contains(podNodeNames, node.Name)
	})

	return targetNodes, nil
}

// ensureTargetGroupForService creates or updates a target group for the given service and its target nodes
func (m *Manager) ensureTargetGroupForService(ctx context.Context, svc corev1.Service, targetNodes []corev1.Node) error {
	serviceKey := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)

	if len(targetNodes) == 0 {
		m.logger.WarnContext(ctx, "No target nodes available for service", "service", serviceKey)
		return nil
	}

	// Check minimum instance count requirement
	if len(targetNodes) < m.cfg.MinInstanceCount {
		m.logger.WarnContext(ctx, "Insufficient target nodes to meet minimum instance count requirement",
			"service", serviceKey,
			"available_nodes", len(targetNodes),
			"min_required", m.cfg.MinInstanceCount)
		return nil
	}

	// Create target group name (must be unique and follow AWS naming rules)
	targetGroupName := fmt.Sprintf("tgs-%s-%s", svc.Namespace, svc.Name)
	if len(targetGroupName) > 32 {
		// Truncate if too long (AWS limit is 32 chars)
		targetGroupName = targetGroupName[:32]
	}

	// Get the NodePort from the service
	var nodePort int32
	for _, port := range svc.Spec.Ports {
		if port.NodePort != 0 {
			nodePort = port.NodePort
			break
		}
	}

	if nodePort == 0 {
		return fmt.Errorf("no NodePort found for service %s", serviceKey)
	}

	// Create target group
	createInput := &elasticloadbalancingv2.CreateTargetGroupInput{
		Name:       aws.String(targetGroupName),
		Port:       aws.Int32(nodePort),
		Protocol:   types.ProtocolEnumTcp, // Default to TCP, could be made configurable
		VpcId:      aws.String(m.cfg.VpcId),
		TargetType: types.TargetTypeEnumIp,
		Tags: []types.Tag{
			{
				Key:   aws.String(TagKeyDeleteKey),
				Value: aws.String(serviceKey),
			},
		},
	}

	m.logger.InfoContext(ctx, "Creating target group", "name", targetGroupName, "service", serviceKey, "nodePort", nodePort)

	if m.cfg.DryRun {
		m.logger.InfoContext(ctx, "DRY RUN: Would create target group", "input", createInput)
		return nil
	}

	// Check if target group already exists
	describeInput := &elasticloadbalancingv2.DescribeTargetGroupsInput{
		Names: []string{targetGroupName},
	}

	describeOutput, err := m.elbv2Client.DescribeTargetGroups(ctx, describeInput)
	if err == nil && len(describeOutput.TargetGroups) > 0 {
		// Target group exists, update targets
		targetGroupArn := *describeOutput.TargetGroups[0].TargetGroupArn
		return m.updateTargetGroupTargets(ctx, targetGroupArn, targetNodes)
	}

	// Create new target group
	createOutput, err := m.elbv2Client.CreateTargetGroup(ctx, createInput)
	if err != nil {
		return fmt.Errorf("failed to create target group for service %s: %w", serviceKey, err)
	}

	targetGroupArn := *createOutput.TargetGroups[0].TargetGroupArn
	m.logger.InfoContext(ctx, "Created target group", "arn", targetGroupArn, "service", serviceKey)

	// Register targets
	return m.updateTargetGroupTargets(ctx, targetGroupArn, targetNodes)
}

// updateTargetGroupTargets updates the targets in a target group
func (m *Manager) updateTargetGroupTargets(ctx context.Context, targetGroupArn string, targetNodes []corev1.Node) error {
	// Prepare targets (using node IP addresses)
	targets := lo.FilterMap(targetNodes, func(node corev1.Node, _ int) (types.TargetDescription, bool) {
		nodeIP := m.extractNodeInternalIP(node)
		if nodeIP == "" {
			m.logger.WarnContext(ctx, "Could not extract internal IP from node", "node", node.Name)
			return types.TargetDescription{}, false
		}
		return types.TargetDescription{
			Id: aws.String(nodeIP),
		}, true
	})

	// Register targets
	registerInput := &elasticloadbalancingv2.RegisterTargetsInput{
		TargetGroupArn: aws.String(targetGroupArn),
		Targets:        targets,
	}

	if m.cfg.DryRun {
		m.logger.InfoContext(ctx, "DRY RUN: Would register targets", "targetGroupArn", targetGroupArn, "targets", len(targets))
		return nil
	}

	_, err := m.elbv2Client.RegisterTargets(ctx, registerInput)
	if err != nil {
		return fmt.Errorf("failed to register targets for target group %s: %w", targetGroupArn, err)
	}

	m.logger.InfoContext(ctx, "Registered targets", "targetGroupArn", targetGroupArn, "targets", len(targets))
	return nil
}

func filterNodePortServices(services []corev1.Service) []corev1.Service {
	return lo.Filter(services, func(svc corev1.Service, _ int) bool {
		return svc.Spec.Type == corev1.ServiceTypeNodePort
	})
}

func (m *Manager) ensureTargetGroupsForNodePortServices(ctx context.Context, services []corev1.Service, readyNodes []corev1.Node) error {
	// Construct mapping of services to their target nodes
	nodePortMap, err := m.constructNodePortMap(ctx, services, readyNodes)
	if err != nil {
		return err
	}

	// Create target groups for each service
	for serviceKey, targetNodes := range nodePortMap {
		// Find the service object for this key
		var targetService *corev1.Service
		for _, svc := range services {
			currentKey := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
			if currentKey == serviceKey {
				targetService = &svc
				break
			}
		}

		if targetService == nil {
			m.logger.WarnContext(ctx, "Could not find service for key", "serviceKey", serviceKey)
			continue
		}

		if err := m.ensureTargetGroupForService(ctx, *targetService, targetNodes); err != nil {
			m.logger.ErrorContext(ctx, "Failed to ensure target group for service", "service", serviceKey, "error", err)
			continue
		}
	}

	return nil
}

// cleanupOrphanedTargetGroups removes target groups that have our delete key tag but no corresponding service
func (m *Manager) cleanupOrphanedTargetGroups(ctx context.Context) error {
	// Get all target groups
	describeInput := &elasticloadbalancingv2.DescribeTargetGroupsInput{}

	describeOutput, err := m.elbv2Client.DescribeTargetGroups(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("failed to describe target groups: %w", err)
	}

	// Get target group ARNs for tag checking
	targetGroupArns := lo.Map(describeOutput.TargetGroups, func(tg types.TargetGroup, _ int) string {
		return *tg.TargetGroupArn
	})

	if len(targetGroupArns) == 0 {
		m.logger.InfoContext(ctx, "No target groups found")
		return nil
	}

	// Get tags for all target groups
	describeTagsInput := &elasticloadbalancingv2.DescribeTagsInput{
		ResourceArns: targetGroupArns,
	}

	tagsOutput, err := m.elbv2Client.DescribeTags(ctx, describeTagsInput)
	if err != nil {
		return fmt.Errorf("failed to describe tags: %w", err)
	}

	// Create map of target group ARN to tags
	tagsByArn := make(map[string][]types.Tag)
	for _, tagDescription := range tagsOutput.TagDescriptions {
		if tagDescription.ResourceArn != nil {
			tagsByArn[*tagDescription.ResourceArn] = tagDescription.Tags
		}
	}

	// Filter target groups that have our delete key tag
	ourTargetGroups := lo.Filter(describeOutput.TargetGroups, func(tg types.TargetGroup, _ int) bool {
		tags, exists := tagsByArn[*tg.TargetGroupArn]
		return exists && m.hasDeleteKeyTag(tags)
	})

	m.logger.InfoContext(ctx, "Found target groups with delete key tag", "count", len(ourTargetGroups))

	// Get all existing services to check against
	serviceList := corev1.ServiceList{}
	listOptions := []client.ListOption{
		client.MatchingLabels{m.cfg.MatchingLabelKey: m.cfg.MatchingLabelValue},
	}

	if err := m.c.List(ctx, &serviceList, listOptions...); err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	// Create a set of existing service keys
	existingServiceKeys := make(map[string]bool)
	for _, svc := range serviceList.Items {
		if svc.Spec.Type == corev1.ServiceTypeNodePort {
			serviceKey := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
			existingServiceKeys[serviceKey] = true
		}
	}

	// Check each target group and delete if orphaned
	var deletedCount int
	for _, tg := range ourTargetGroups {
		tags := tagsByArn[*tg.TargetGroupArn]
		serviceKey := m.getDeleteKeyFromTags(tags)
		if serviceKey == "" {
			continue
		}

		if !existingServiceKeys[serviceKey] {
			// Service no longer exists, delete target group
			if err := m.deleteTargetGroup(ctx, *tg.TargetGroupArn, serviceKey); err != nil {
				m.logger.ErrorContext(ctx, "Failed to delete orphaned target group", "arn", *tg.TargetGroupArn, "service", serviceKey, "error", err)
				continue
			}
			deletedCount++
		}
	}

	m.logger.InfoContext(ctx, "Cleaned up orphaned target groups", "deleted", deletedCount)
	return nil
}

// hasDeleteKeyTag checks if the target group has our delete key tag
func (m *Manager) hasDeleteKeyTag(tags []types.Tag) bool {
	for _, tag := range tags {
		if tag.Key != nil && *tag.Key == TagKeyDeleteKey {
			return true
		}
	}
	return false
}

// getDeleteKeyFromTags extracts the delete key value from target group tags
func (m *Manager) getDeleteKeyFromTags(tags []types.Tag) string {
	for _, tag := range tags {
		if tag.Key != nil && *tag.Key == TagKeyDeleteKey && tag.Value != nil {
			return *tag.Value
		}
	}
	return ""
}

// deleteTargetGroup deletes a target group
func (m *Manager) deleteTargetGroup(ctx context.Context, targetGroupArn, serviceKey string) error {
	m.logger.InfoContext(ctx, "Deleting orphaned target group", "arn", targetGroupArn, "service", serviceKey)

	if m.cfg.DryRun {
		m.logger.InfoContext(ctx, "DRY RUN: Would delete target group", "arn", targetGroupArn, "service", serviceKey)
		return nil
	}

	deleteInput := &elasticloadbalancingv2.DeleteTargetGroupInput{
		TargetGroupArn: aws.String(targetGroupArn),
	}

	_, err := m.elbv2Client.DeleteTargetGroup(ctx, deleteInput)
	if err != nil {
		return fmt.Errorf("failed to delete target group %s: %w", targetGroupArn, err)
	}

	m.logger.InfoContext(ctx, "Deleted orphaned target group", "arn", targetGroupArn, "service", serviceKey)
	return nil
}

// extractInstanceIDFromProviderID extracts the EC2 instance ID from a Kubernetes node providerID
// Expected format: aws:///zone/instance-id or aws://zone/instance-id
func (m *Manager) extractInstanceIDFromProviderID(providerID string) string {
	if providerID == "" {
		return ""
	}

	// Handle AWS provider ID format: aws:///zone/instance-id
	if strings.HasPrefix(providerID, "aws://") {
		parts := strings.Split(providerID, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-1] // Get the last part which should be the instance ID
		}
	}

	return ""
}

// extractNodeInternalIP extracts the internal IP address from a Kubernetes node
func (m *Manager) extractNodeInternalIP(node corev1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address
		}
	}
	return ""
}
