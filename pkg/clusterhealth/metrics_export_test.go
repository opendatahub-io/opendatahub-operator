package clusterhealth

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPrometheusExport_Nodes(t *testing.T) {
	cpuUsage := int64(2500)
	memUsage := int64(4294967296)
	report := &Report{
		CollectedAt: time.Unix(1710849600, 0),
		Nodes: SectionResult[NodesSection]{
			Data: NodesSection{
				Nodes: []NodeInfo{
					{
						Name:               "worker-0",
						Role:               "worker",
						UsageCPUMillicores: &cpuUsage,
						UsageMemoryBytes:   &memUsage,
						Conditions: []ConditionSummary{
							{Type: "Ready", Status: "True"},
							{Type: "MemoryPressure", Status: "False"},
						},
					},
				},
				Data: []corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "worker-0",
							Labels: map[string]string{
								"node-role.kubernetes.io/worker": "",
							},
						},
						Status: corev1.NodeStatus{
							Allocatable: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("4"),
								corev1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Capacity: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("4"),
								corev1.ResourceMemory: resource.MustParse("8Gi"),
							},
						},
					},
				},
			},
		},
	}

	lines := report.PrometheusExport()
	assertContainsLine(t, lines, `kube_node_role{node="worker-0",role="worker"} 1 1710849600000`)
	assertContainsLine(t, lines, `kube_node_status_allocatable{node="worker-0",resource="cpu",unit="core"} 4 1710849600000`)
	assertContainsLine(t, lines, `kube_node_status_allocatable{node="worker-0",resource="memory",unit="byte"} 8589934592 1710849600000`)
	assertContainsLine(t, lines, `kube_node_status_capacity{node="worker-0",resource="cpu",unit="core"} 4 1710849600000`)
	assertContainsLine(t, lines, `machine_cpu_cores{node="worker-0"} 4 1710849600000`)
	assertContainsLine(t, lines, `kube_node_status_capacity{node="worker-0",resource="memory",unit="byte"} 8589934592 1710849600000`)
	assertContainsLine(t, lines, `node_memory_MemTotal_bytes{node="worker-0"} 8589934592 1710849600000`)
	assertContainsLine(t, lines, `node_cpu_usage_cores{node="worker-0"} 2.5 1710849600000`)
	assertContainsLine(t, lines, `container_memory_working_set_bytes{id="/",node="worker-0"} 4294967296 1710849600000`)
	assertContainsLine(t, lines, `kube_node_status_condition{condition="Ready",node="worker-0",status="true"} 1 1710849600000`)
	assertContainsLine(t, lines, `kube_node_status_condition{condition="MemoryPressure",node="worker-0",status="false"} 0 1710849600000`)
}

func TestPrometheusExport_NodesWithoutMetricsServer(t *testing.T) {
	report := &Report{
		CollectedAt: time.Unix(1710849600, 0),
		Nodes: SectionResult[NodesSection]{
			Data: NodesSection{
				Nodes: []NodeInfo{
					{
						Name:               "master-0",
						Role:               "master",
						UsageCPUMillicores: nil,
						UsageMemoryBytes:   nil,
					},
				},
				Data: []corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "master-0",
							Labels: map[string]string{
								"node-role.kubernetes.io/master": "",
							},
						},
						Status: corev1.NodeStatus{
							Allocatable: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("4"),
							},
						},
					},
				},
			},
		},
	}

	lines := report.PrometheusExport()
	assertContainsLine(t, lines, `kube_node_status_allocatable{node="master-0",resource="cpu",unit="core"} 4 1710849600000`)
	assertNotContainsPrefix(t, lines, `node_cpu_usage_cores`)
	assertNotContainsPrefix(t, lines, `container_memory_working_set_bytes`)
}

func TestPrometheusExport_Deployments(t *testing.T) {
	report := &Report{
		CollectedAt: time.Unix(1710849600, 0),
		Deployments: SectionResult[DeploymentsSection]{
			Data: DeploymentsSection{
				ByNamespace: map[string][]DeploymentInfo{
					"opendatahub": {
						{Namespace: "opendatahub", Name: "dashboard", Ready: 3, Replicas: 3},
						{Namespace: "opendatahub", Name: "notebook-controller", Ready: 1, Replicas: 2},
					},
				},
			},
		},
	}

	lines := report.PrometheusExport()
	assertContainsLine(t, lines, `kube_deployment_status_replicas{deployment="dashboard",namespace="opendatahub"} 3 1710849600000`)
	assertContainsLine(t, lines, `kube_deployment_status_replicas_ready{deployment="dashboard",namespace="opendatahub"} 3 1710849600000`)
	assertContainsLine(t, lines, `kube_deployment_status_replicas{deployment="notebook-controller",namespace="opendatahub"} 2 1710849600000`)
	assertContainsLine(t, lines, `kube_deployment_status_replicas_ready{deployment="notebook-controller",namespace="opendatahub"} 1 1710849600000`)
}

func TestPrometheusExport_Pods(t *testing.T) {
	reqCPU := int64(100)
	reqMem := int64(1024)
	limCPU := int64(500)
	limMem := int64(2048)

	report := &Report{
		CollectedAt: time.Unix(1710849600, 0),
		Pods: SectionResult[PodsSection]{
			Data: PodsSection{
				ByNamespace: map[string][]PodInfo{
					"opendatahub": {
						{
							Namespace: "opendatahub",
							Name:      "dashboard-abc",
							Phase:     "Running",
							NodeName:  "worker-0",
							Containers: []ContainerInfo{
								{
									Name:           "dashboard",
									Ready:          true,
									RestartCount:   0,
									RequestsCPU:    &reqCPU,
									RequestsMemory: &reqMem,
									LimitsCPU:      &limCPU,
									LimitsMemory:   &limMem,
								},
								{Name: "oauth-proxy", Ready: false, RestartCount: 3},
							},
						},
					},
				},
			},
		},
	}

	lines := report.PrometheusExport()
	assertContainsLine(t, lines, `kube_pod_status_phase{namespace="opendatahub",phase="Running",pod="dashboard-abc"} 1 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_status_phase{namespace="opendatahub",phase="Pending",pod="dashboard-abc"} 0 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_status_phase{namespace="opendatahub",phase="Succeeded",pod="dashboard-abc"} 0 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_status_phase{namespace="opendatahub",phase="Failed",pod="dashboard-abc"} 0 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_status_phase{namespace="opendatahub",phase="Unknown",pod="dashboard-abc"} 0 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_container_status_ready{container="dashboard",namespace="opendatahub",pod="dashboard-abc"} 1 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_container_status_restarts_total{container="dashboard",namespace="opendatahub",pod="dashboard-abc"} 0 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_container_resource_requests{container="dashboard",namespace="opendatahub",`+
		`node="worker-0",pod="dashboard-abc",resource="cpu",unit="core"} 0.1 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_container_resource_requests{container="dashboard",namespace="opendatahub",`+
		`node="worker-0",pod="dashboard-abc",resource="memory",unit="byte"} 1024 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_container_resource_limits{container="dashboard",namespace="opendatahub",`+
		`node="worker-0",pod="dashboard-abc",resource="cpu",unit="core"} 0.5 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_container_resource_limits{container="dashboard",namespace="opendatahub",`+
		`node="worker-0",pod="dashboard-abc",resource="memory",unit="byte"} 2048 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_container_status_ready{container="oauth-proxy",namespace="opendatahub",pod="dashboard-abc"} 0 1710849600000`)
	assertContainsLine(t, lines, `kube_pod_container_status_restarts_total{container="oauth-proxy",namespace="opendatahub",pod="dashboard-abc"} 3 1710849600000`)
}

func TestPrometheusExport_Quotas(t *testing.T) {
	report := &Report{
		CollectedAt: time.Unix(1710849600, 0),
		Quotas: SectionResult[QuotasSection]{
			Data: QuotasSection{
				Data: []corev1.ResourceQuota{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "compute", Namespace: "opendatahub"},
						Status: corev1.ResourceQuotaStatus{
							Hard: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("10"),
							},
							Used: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("3"),
							},
						},
					},
				},
			},
		},
	}

	lines := report.PrometheusExport()
	assertContainsLine(t, lines, `kube_resourcequota{namespace="opendatahub",quota="compute",resource="cpu",type="hard"} 10 1710849600000`)
	assertContainsLine(t, lines, `kube_resourcequota{namespace="opendatahub",quota="compute",resource="cpu",type="used"} 3 1710849600000`)
}

func TestPrometheusExport_Health(t *testing.T) {
	t.Run("healthy cluster", func(t *testing.T) {
		report := &Report{CollectedAt: time.Unix(1710849600, 0)}
		lines := report.PrometheusExport()
		assertContainsLine(t, lines, `cluster_healthy 1 1710849600000`)
		assertContainsLine(t, lines, `section_healthy{section="nodes"} 1 1710849600000`)
	})

	t.Run("unhealthy cluster", func(t *testing.T) {
		report := &Report{
			CollectedAt: time.Unix(1710849600, 0),
			Nodes:       SectionResult[NodesSection]{Error: "unhealthy nodes: node-1 (MemoryPressure)"},
		}
		lines := report.PrometheusExport()
		assertContainsLine(t, lines, `cluster_healthy 0 1710849600000`)
		assertContainsLine(t, lines, `section_healthy{section="nodes"} 0 1710849600000`)
		assertContainsLine(t, lines, `section_healthy{section="deployments"} 1 1710849600000`)
	})
}

func TestPrometheusExport_EmptyReport(t *testing.T) {
	report := &Report{CollectedAt: time.Unix(1710849600, 0)}
	lines := report.PrometheusExport()
	if len(lines) == 0 {
		t.Error("PrometheusExport should always emit at least health metrics")
	}
	assertContainsLine(t, lines, `cluster_healthy 1 1710849600000`)
}

func TestPromLine(t *testing.T) {
	got := promLine("test_metric", promLabels{"a": "1", "b": "2"}, 42.5, 1710849600000)
	want := `test_metric{a="1",b="2"} 42.5 1710849600000`
	if got != want {
		t.Errorf("promLine() = %q, want %q", got, want)
	}
}

func TestPromLine_NoLabels(t *testing.T) {
	got := promLine("test_metric", nil, 1, 1710849600000)
	want := `test_metric 1 1710849600000`
	if got != want {
		t.Errorf("promLine() = %q, want %q", got, want)
	}
}

func TestEscapePromLabelValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{`has "quotes"`, `has \"quotes\"`},
		{"has\nnewline", `has\nnewline`},
		{`has\backslash`, `has\\backslash`},
	}
	for _, tt := range tests {
		got := escapePromLabelValue(tt.input)
		if got != tt.want {
			t.Errorf("escapePromLabelValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{3.14, "3.14"},
		{0.001, "0.001"},
	}
	for _, tt := range tests {
		got := formatFloat(tt.input)
		if got != tt.want {
			t.Errorf("formatFloat(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func assertContainsLine(t *testing.T, lines []string, want string) {
	t.Helper()
	for _, line := range lines {
		if line == want {
			return
		}
	}
	t.Errorf("expected line not found: %s\ngot lines:\n  %s", want, strings.Join(lines, "\n  "))
}

func assertNotContainsPrefix(t *testing.T, lines []string, prefix string) {
	t.Helper()
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			t.Errorf("unexpected line with prefix %q found: %s", prefix, line)
		}
	}
}
