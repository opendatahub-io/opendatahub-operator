package clusterhealth

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func runNodesSection(ctx context.Context, c client.Client, _ NamespaceConfig) SectionResult[NodesSection] {
	var out SectionResult[NodesSection]

	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes); err != nil {
		out.Error = fmt.Sprintf("failed to list nodes: %v", err)
		return out
	}

	if len(nodes.Items) == 0 {
		out.Data.Nodes = []NodeInfo{}
		return out
	}

	usageByNode := fetchNodeMetrics(ctx, c)

	out.Data.Data = nodes.Items
	out.Data.Nodes = make([]NodeInfo, 0, len(nodes.Items))
	for i := range nodes.Items {
		node := &nodes.Items[i]
		info := nodeToNodeInfo(node)
		if usage, ok := usageByNode[node.Name]; ok {
			if cpu, hasCpu := usage[corev1.ResourceCPU]; hasCpu {
				cpuMilli := cpu.MilliValue()
				info.UsageCPUMillicores = &cpuMilli
			}
			if mem, hasMem := usage[corev1.ResourceMemory]; hasMem {
				memBytes := mem.Value()
				info.UsageMemoryBytes = &memBytes
			}
		}
		out.Data.Nodes = append(out.Data.Nodes, info)
	}

	var unhealthy []string
	for _, info := range out.Data.Nodes {
		if info.UnhealthyReason != "" {
			unhealthy = append(unhealthy, fmt.Sprintf("%s (%s)", info.Name, info.UnhealthyReason))
		}
	}
	if len(unhealthy) > 0 {
		out.Error = fmt.Sprintf("unhealthy nodes: %s", strings.Join(unhealthy, "; "))
	}
	return out
}

func nodeToNodeInfo(node *corev1.Node) NodeInfo {
	info := NodeInfo{Name: node.Name}

	const rolePrefix = "node-role.kubernetes.io/"
	var roles []string
	for key := range node.Labels {
		if strings.HasPrefix(key, rolePrefix) {
			roles = append(roles, strings.TrimPrefix(key, rolePrefix))
		}
	}
	sort.Strings(roles)
	info.Role = strings.Join(roles, ",")

	for _, c := range node.Status.Conditions {
		info.Conditions = append(info.Conditions, ConditionSummary{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Message: c.Message,
		})
	}

	info.Allocatable = formatResourceList(node.Status.Allocatable)
	info.Capacity = formatResourceList(node.Status.Capacity)

	var pressureReasons []string
	var notReady string
	var otherReasons []string
	for _, c := range node.Status.Conditions {
		switch c.Type {
		case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure:
			if c.Status == corev1.ConditionTrue {
				pressureReasons = append(pressureReasons, string(c.Type))
			}
		case corev1.NodeReady:
			if c.Status == corev1.ConditionFalse || c.Status == corev1.ConditionUnknown {
				notReady = "Ready=" + string(c.Status)
			}
		case corev1.NodeNetworkUnavailable:
			if c.Status == corev1.ConditionTrue {
				otherReasons = append(otherReasons, "network unavailable")
			}
		}
	}
	var parts []string
	if len(pressureReasons) > 0 {
		parts = append(parts, "resource pressure: "+strings.Join(pressureReasons, ", "))
	}
	if notReady != "" {
		parts = append(parts, notReady)
	}
	if len(otherReasons) > 0 {
		parts = append(parts, strings.Join(otherReasons, ", "))
	}
	if len(parts) > 0 {
		info.UnhealthyReason = strings.Join(parts, "; ")
	}
	return info
}

// fetchNodeMetrics uses unstructured to get node metrics without requiring the k8s.io/metrics dependency.
// Returns an empty map if the Metrics Server is not available.
func fetchNodeMetrics(ctx context.Context, c client.Client) map[string]corev1.ResourceList {
	uList := &unstructured.UnstructuredList{}
	uList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "metrics.k8s.io",
		Version: "v1beta1",
		Kind:    "NodeMetricsList",
	})

	if err := c.List(ctx, uList); err != nil {
		log.Printf("clusterhealth: node metrics unavailable (Metrics Server may not be installed): %v", err)
		return nil
	}

	m := make(map[string]corev1.ResourceList, len(uList.Items))
	for _, item := range uList.Items {
		name := item.GetName()

		usageMap, found, _ := unstructured.NestedStringMap(item.Object, "usage")
		if !found {
			continue
		}

		rl := corev1.ResourceList{}
		if cpuStr, ok := usageMap["cpu"]; ok {
			if q, err := resource.ParseQuantity(cpuStr); err == nil {
				rl[corev1.ResourceCPU] = q
			}
		}
		if memStr, ok := usageMap["memory"]; ok {
			if q, err := resource.ParseQuantity(memStr); err == nil {
				rl[corev1.ResourceMemory] = q
			}
		}
		m[name] = rl
	}
	return m
}

func formatResourceList(rl corev1.ResourceList) string {
	var parts []string
	if cpu := rl[corev1.ResourceCPU]; !cpu.IsZero() {
		parts = append(parts, fmt.Sprintf("%dm CPU", cpu.MilliValue()))
	}
	if mem := rl[corev1.ResourceMemory]; !mem.IsZero() {
		parts = append(parts, fmt.Sprintf("%s memory", mem.String()))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}
