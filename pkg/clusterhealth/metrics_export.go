package clusterhealth

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// PrometheusExport converts the Report to Prometheus exposition format lines.
// Each line includes the CollectedAt timestamp in Unix seconds.
// The output can be appended to a file for bulk import into VictoriaMetrics
// via /api/v1/import/prometheus.
func (r *Report) PrometheusExport() []string {
	ts := r.CollectedAt.UnixMilli()
	var lines []string

	lines = append(lines, r.exportNodes(ts)...)
	lines = append(lines, r.exportDeployments(ts)...)
	lines = append(lines, r.exportPods(ts)...)
	lines = append(lines, r.exportQuotas(ts)...)
	lines = append(lines, r.exportHealth(ts)...)

	return lines
}

func (r *Report) exportNodes(ts int64) []string {
	var lines []string

	rawByName := make(map[string]*corev1.Node, len(r.Nodes.Data.Data))
	for i := range r.Nodes.Data.Data {
		node := &r.Nodes.Data.Data[i]
		rawByName[node.Name] = node
	}

	// Build a sorted list of node names for deterministic output order.
	names := make([]string, 0, len(r.Nodes.Data.Nodes))
	infoByName := make(map[string]*NodeInfo, len(r.Nodes.Data.Nodes))
	for i := range r.Nodes.Data.Nodes {
		info := &r.Nodes.Data.Nodes[i]
		infoByName[info.Name] = info
		names = append(names, info.Name)
	}
	sort.Strings(names)

	for _, name := range names {
		info := infoByName[name]
		labels := promLabels{"node": name}

		if info.Role != "" {
			lines = append(lines, promLine("kube_node_role", promLabels{"node": name, "role": info.Role}, 1.0, ts))
		}

		if node, ok := rawByName[name]; ok {
			if cpu := node.Status.Allocatable[corev1.ResourceCPU]; !cpu.IsZero() {
				val := float64(cpu.MilliValue()) / 1000.0
				lines = append(lines, promLine("kube_node_status_allocatable", promLabels{"node": name, "resource": "cpu", "unit": "core"}, val, ts))
			}
			if mem := node.Status.Allocatable[corev1.ResourceMemory]; !mem.IsZero() {
				val := float64(mem.Value())
				lines = append(lines, promLine("kube_node_status_allocatable", promLabels{"node": name, "resource": "memory", "unit": "byte"}, val, ts))
			}
			if cpu := node.Status.Capacity[corev1.ResourceCPU]; !cpu.IsZero() {
				val := float64(cpu.MilliValue()) / 1000.0
				lines = append(lines, promLine("kube_node_status_capacity", promLabels{"node": name, "resource": "cpu", "unit": "core"}, val, ts))
				lines = append(lines, promLine("machine_cpu_cores", labels, val, ts))
			}
			if mem := node.Status.Capacity[corev1.ResourceMemory]; !mem.IsZero() {
				val := float64(mem.Value())
				lines = append(lines, promLine("kube_node_status_capacity", promLabels{"node": name, "resource": "memory", "unit": "byte"}, val, ts))
				lines = append(lines, promLine("node_memory_MemTotal_bytes", labels, val, ts))
			}
		}

		if info.UsageCPUMillicores != nil {
			val := float64(*info.UsageCPUMillicores) / 1000.0
			lines = append(lines, promLine("node_cpu_usage_cores", labels, val, ts))
		}
		// Metrics API reports RSS + cache, not exactly working_set (which excludes inactive
		// file cache). We use the cAdvisor metric name for dashboard compatibility.
		if info.UsageMemoryBytes != nil {
			val := float64(*info.UsageMemoryBytes)
			lines = append(lines, promLine("container_memory_working_set_bytes", promLabels{"node": name, "id": "/"}, val, ts))
		}

		for _, cond := range info.Conditions {
			statusLower := strings.ToLower(cond.Status)
			condLabels := promLabels{"node": name, "condition": cond.Type, "status": statusLower}
			val := 0.0
			if cond.Status == "True" {
				val = 1.0
			}
			lines = append(lines, promLine("kube_node_status_condition", condLabels, val, ts))
		}
	}

	return lines
}

func (r *Report) exportDeployments(ts int64) []string {
	var lines []string
	for _, ns := range sortedMapKeys(r.Deployments.Data.ByNamespace) {
		for _, d := range r.Deployments.Data.ByNamespace[ns] {
			labels := promLabels{"namespace": d.Namespace, "deployment": d.Name}
			lines = append(lines, promLine("kube_deployment_status_replicas", labels, float64(d.Replicas), ts))
			lines = append(lines, promLine("kube_deployment_status_replicas_ready", labels, float64(d.Ready), ts))
		}
	}
	return lines
}

var podPhases = []string{"Pending", "Running", "Succeeded", "Failed", "Unknown"}

func (r *Report) exportPods(ts int64) []string {
	var lines []string
	for _, ns := range sortedMapKeys(r.Pods.Data.ByNamespace) {
		for _, pod := range r.Pods.Data.ByNamespace[ns] {
			for _, phase := range podPhases {
				val := 0.0
				if pod.Phase == phase {
					val = 1.0
				}
				lines = append(lines, promLine("kube_pod_status_phase", promLabels{"namespace": pod.Namespace, "pod": pod.Name, "phase": phase}, val, ts))
			}

			for _, c := range pod.Containers {
				labels := promLabels{"namespace": pod.Namespace, "pod": pod.Name, "container": c.Name}
				lines = append(lines, promLine("kube_pod_container_status_restarts_total", labels, float64(c.RestartCount), ts))
				readyVal := 0.0
				if c.Ready {
					readyVal = 1.0
				}
				lines = append(lines, promLine("kube_pod_container_status_ready", labels, readyVal, ts))

				resLabels := promLabels{"namespace": pod.Namespace, "pod": pod.Name, "container": c.Name}
				if pod.NodeName != "" {
					resLabels["node"] = pod.NodeName
				}

				cpuLabels := mergeLabels(resLabels, promLabels{"resource": "cpu", "unit": "core"})
				memLabels := mergeLabels(resLabels, promLabels{"resource": "memory", "unit": "byte"})

				if c.RequestsCPU != nil {
					val := float64(*c.RequestsCPU) / 1000.0
					lines = append(lines, promLine("kube_pod_container_resource_requests", cpuLabels, val, ts))
				}
				if c.RequestsMemory != nil {
					lines = append(lines, promLine("kube_pod_container_resource_requests", memLabels, float64(*c.RequestsMemory), ts))
				}
				if c.LimitsCPU != nil {
					val := float64(*c.LimitsCPU) / 1000.0
					lines = append(lines, promLine("kube_pod_container_resource_limits", cpuLabels, val, ts))
				}
				if c.LimitsMemory != nil {
					lines = append(lines, promLine("kube_pod_container_resource_limits", memLabels, float64(*c.LimitsMemory), ts))
				}
			}
		}
	}
	return lines
}

func (r *Report) exportQuotas(ts int64) []string {
	var lines []string
	for _, quota := range r.Quotas.Data.Data {
		for _, res := range sortedResourceNames(quota.Status.Hard) {
			qty := quota.Status.Hard[corev1.ResourceName(res)]
			labels := promLabels{"namespace": quota.Namespace, "quota": quota.Name, "resource": res, "type": "hard"}
			lines = append(lines, promLine("kube_resourcequota", labels, quantityToFloat(qty), ts))
		}
		for _, res := range sortedResourceNames(quota.Status.Used) {
			qty := quota.Status.Used[corev1.ResourceName(res)]
			labels := promLabels{"namespace": quota.Namespace, "quota": quota.Name, "resource": res, "type": "used"}
			lines = append(lines, promLine("kube_resourcequota", labels, quantityToFloat(qty), ts))
		}
	}
	return lines
}

func (r *Report) exportHealth(ts int64) []string {
	healthy := 0.0
	if r.Healthy() {
		healthy = 1.0
	}
	lines := []string{promLine("cluster_healthy", nil, healthy, ts)}

	sectionErrors := map[string]string{
		SectionNodes:       r.Nodes.Error,
		SectionDeployments: r.Deployments.Error,
		SectionPods:        r.Pods.Error,
		SectionEvents:      r.Events.Error,
		SectionQuotas:      r.Quotas.Error,
		SectionOperator:    r.Operator.Error,
		SectionDSCI:        r.DSCI.Error,
		SectionDSC:         r.DSC.Error,
	}
	for _, section := range sortedMapKeys(sectionErrors) {
		val := 1.0
		if sectionErrors[section] != "" {
			val = 0.0
		}
		lines = append(lines, promLine("section_healthy", promLabels{"section": section}, val, ts))
	}

	return lines
}

// promLabels is label key-value pairs for a Prometheus metric line.
type promLabels map[string]string

// promLine formats a single Prometheus exposition line:
// metric_name{label1="val1",label2="val2"} value timestamp_seconds.
func promLine(name string, labels promLabels, value float64, ts int64) string {
	labelsStr := formatPromLabels(labels)

	valueStr := formatFloat(value)
	return fmt.Sprintf("%s%s %s %d", name, labelsStr, valueStr, ts)
}

func formatPromLabels(labels promLabels) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, escapePromLabelValue(labels[k])))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// escapePromLabelValue escapes characters per the Prometheus exposition format spec
// (\, ", \n). We also escape \r defensively since node condition messages can
// contain carriage returns on some platforms.
func escapePromLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return s
}

func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return fmt.Sprintf("%g", v)
}

func quantityToFloat(q resource.Quantity) float64 {
	return q.AsApproximateFloat64()
}

func mergeLabels(base, extra promLabels) promLabels {
	merged := make(promLabels, len(base)+len(extra))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedResourceNames(rl corev1.ResourceList) []string {
	keys := make([]string, 0, len(rl))
	for k := range rl {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	return keys
}
