//nolint:testpackage
package classifier

import (
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
)

func assertClassification(t *testing.T, got FailureClassification, wantCategory, wantSubcategory string, wantErrorCode int, wantConfidence string) {
	t.Helper()

	if got.Category != wantCategory {
		t.Errorf("Category = %q, want %q", got.Category, wantCategory)
	}
	if got.Subcategory != wantSubcategory {
		t.Errorf("Subcategory = %q, want %q", got.Subcategory, wantSubcategory)
	}
	if got.ErrorCode != wantErrorCode {
		t.Errorf("ErrorCode = %d, want %d", got.ErrorCode, wantErrorCode)
	}
	if got.Confidence != wantConfidence {
		t.Errorf("Confidence = %q, want %q", got.Confidence, wantConfidence)
	}
	if len(got.Evidence) == 0 {
		t.Error("Evidence should not be empty")
	}
}

func TestClassify_NilAndCleanReport(t *testing.T) {
	tests := []struct {
		name            string
		report          *clusterhealth.Report
		wantCategory    string
		wantSubcategory string
		wantErrorCode   int
		wantConfidence  string
	}{
		{
			name:            "nil report returns unknown",
			report:          nil,
			wantCategory:    CategoryUnknown,
			wantSubcategory: "unclassifiable",
			wantErrorCode:   CodeUnclassifiable,
			wantConfidence:  ConfidenceLow,
		},
		{
			name:            "clean report classifies as test failure",
			report:          &clusterhealth.Report{},
			wantCategory:    CategoryTest,
			wantSubcategory: "test-failure",
			wantErrorCode:   CodeTestFailure,
			wantConfidence:  ConfidenceMedium,
		},
		{
			name: "all sections errored returns unknown",
			report: &clusterhealth.Report{
				Pods:        clusterhealth.SectionResult[clusterhealth.PodsSection]{Error: "fail"},
				Events:      clusterhealth.SectionResult[clusterhealth.EventsSection]{Error: "fail"},
				Quotas:      clusterhealth.SectionResult[clusterhealth.QuotasSection]{Error: "fail"},
				Nodes:       clusterhealth.SectionResult[clusterhealth.NodesSection]{Error: "fail"},
				Deployments: clusterhealth.SectionResult[clusterhealth.DeploymentsSection]{Error: "fail"},
			},
			wantCategory:    CategoryUnknown,
			wantSubcategory: "unclassifiable",
			wantErrorCode:   CodeUnclassifiable,
			wantConfidence:  ConfidenceLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.report)
			assertClassification(t, got, tt.wantCategory, tt.wantSubcategory, tt.wantErrorCode, tt.wantConfidence)
		})
	}
}

func TestClassify_ImagePull(t *testing.T) {
	tests := []struct {
		name   string
		report *clusterhealth.Report
	}{
		{
			name: "ImagePullBackOff in waiting container",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{
									Name:  "test-pod",
									Phase: "Running",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "main", Waiting: `Back-off pulling image "quay.io/foo:bad" ImagePullBackOff`},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "ErrImagePull in waiting container",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{
									Name: "test-pod",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "main", Waiting: "ErrImagePull: unable to pull image"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.report)
			assertClassification(t, got, CategoryInfrastructure, "image-pull", CodeImagePull, ConfidenceMedium)
		})
	}
}

func TestClassify_PodStartup(t *testing.T) {
	tests := []struct {
		name           string
		report         *clusterhealth.Report
		wantConfidence string
	}{
		{
			name: "CrashLoopBackOff in waiting container",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{
									Name: "crashing-pod",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "main", Waiting: "CrashLoopBackOff: back-off 5m0s restarting failed container"},
									},
								},
							},
						},
					},
				},
			},
			wantConfidence: ConfidenceMedium,
		},
		{
			name: "pod stuck in Pending",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{Name: "pending-pod", Phase: "Pending"},
							},
						},
					},
				},
			},
			wantConfidence: ConfidenceHigh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.report)
			assertClassification(t, got, CategoryInfrastructure, "pod-startup", CodePodStartup, tt.wantConfidence)
		})
	}
}

func TestClassify_OOMKilled(t *testing.T) {
	report := &clusterhealth.Report{
		Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
			Data: clusterhealth.PodsSection{
				ByNamespace: map[string][]clusterhealth.PodInfo{
					"test-ns": {
						{
							Name: "oom-pod",
							Containers: []clusterhealth.ContainerInfo{
								{Name: "main", Terminated: "OOMKilled"},
							},
						},
					},
				},
			},
		},
	}

	got := Classify(report)
	assertClassification(t, got, CategoryInfrastructure, "quota-oom", CodeQuotaOOM, ConfidenceMedium)
}

func TestClassify_Events(t *testing.T) {
	tests := []struct {
		name            string
		report          *clusterhealth.Report
		wantSubcategory string
		wantErrorCode   int
	}{
		{
			name: "NetworkNotReady event",
			report: &clusterhealth.Report{
				Events: clusterhealth.SectionResult[clusterhealth.EventsSection]{
					Data: clusterhealth.EventsSection{
						Events: []clusterhealth.EventInfo{
							{Kind: "Node", Name: "worker-1", Reason: "NetworkNotReady", Message: "network plugin not ready"},
						},
					},
				},
			},
			wantSubcategory: "network",
			wantErrorCode:   CodeNetwork,
		},
		{
			name: "network not ready in event message",
			report: &clusterhealth.Report{
				Events: clusterhealth.SectionResult[clusterhealth.EventsSection]{
					Data: clusterhealth.EventsSection{
						Events: []clusterhealth.EventInfo{
							{Kind: "Pod", Name: "test-pod", Reason: "FailedScheduling", Message: "network not ready on node"},
						},
					},
				},
			},
			wantSubcategory: "network",
			wantErrorCode:   CodeNetwork,
		},
		{
			name: "FailedAttachVolume event",
			report: &clusterhealth.Report{
				Events: clusterhealth.SectionResult[clusterhealth.EventsSection]{
					Data: clusterhealth.EventsSection{
						Events: []clusterhealth.EventInfo{
							{Kind: "Pod", Name: "test-pod", Reason: "FailedAttachVolume", Message: "Multi-Attach error for volume pvc-123"},
						},
					},
				},
			},
			wantSubcategory: "storage",
			wantErrorCode:   CodeStorage,
		},
		{
			name: "FailedMount event",
			report: &clusterhealth.Report{
				Events: clusterhealth.SectionResult[clusterhealth.EventsSection]{
					Data: clusterhealth.EventsSection{
						Events: []clusterhealth.EventInfo{
							{Kind: "Pod", Name: "test-pod", Reason: "FailedMount", Message: "MountVolume.SetUp failed for volume data"},
						},
					},
				},
			},
			wantSubcategory: "storage",
			wantErrorCode:   CodeStorage,
		},
		{
			name: "storage pattern in event message",
			report: &clusterhealth.Report{
				Events: clusterhealth.SectionResult[clusterhealth.EventsSection]{
					Data: clusterhealth.EventsSection{
						Events: []clusterhealth.EventInfo{
							{Kind: "Pod", Name: "test-pod", Reason: "FailedScheduling", Message: "persistentvolumeclaim \"data-pvc\" not found"},
						},
					},
				},
			},
			wantSubcategory: "storage",
			wantErrorCode:   CodeStorage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.report)
			assertClassification(t, got, CategoryInfrastructure, tt.wantSubcategory, tt.wantErrorCode, ConfidenceMedium)
		})
	}
}

func TestClassify_Quota(t *testing.T) {
	report := &clusterhealth.Report{
		Quotas: clusterhealth.SectionResult[clusterhealth.QuotasSection]{
			Data: clusterhealth.QuotasSection{
				ByNamespace: map[string][]clusterhealth.ResourceQuotaInfo{
					"test-ns": {
						{
							Namespace: "test-ns",
							Name:      "compute-quota",
							Exceeded:  []string{"cpu"},
						},
					},
				},
			},
		},
	}

	got := Classify(report)
	assertClassification(t, got, CategoryInfrastructure, "quota-oom", CodeQuotaOOM, ConfidenceHigh)
}

func TestClassify_NodePressure(t *testing.T) {
	report := &clusterhealth.Report{
		Nodes: clusterhealth.SectionResult[clusterhealth.NodesSection]{
			Data: clusterhealth.NodesSection{
				Nodes: []clusterhealth.NodeInfo{
					{Name: "worker-1", UnhealthyReason: "MemoryPressure: True"},
				},
			},
		},
	}

	got := Classify(report)
	assertClassification(t, got, CategoryInfrastructure, "node-pressure", CodeNodePressure, ConfidenceHigh)
}

func TestClassify_ClusterDistress(t *testing.T) {
	tests := []struct {
		name   string
		report *clusterhealth.Report
	}{
		{
			name: "unrecognized waiting reason",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{
									Name: "broken-pod",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "main", Waiting: "RunContainerError: something went wrong"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "unrecognized terminated reason",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{
									Name: "error-pod",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "main", Terminated: "Error: exit code 1"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "unready deployment",
			report: &clusterhealth.Report{
				Deployments: clusterhealth.SectionResult[clusterhealth.DeploymentsSection]{
					Data: clusterhealth.DeploymentsSection{
						ByNamespace: map[string][]clusterhealth.DeploymentInfo{
							"test-ns": {
								{Namespace: "test-ns", Name: "my-deploy", Ready: 0, Replicas: 3},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.report)
			assertClassification(t, got, CategoryInfrastructure, "cluster-distress", CodeInfraUnknown, ConfidenceLow)
		})
	}
}

func TestClassify_Priority(t *testing.T) {
	tests := []struct {
		name            string
		report          *clusterhealth.Report
		wantSubcategory string
		wantErrorCode   int
		wantConfidence  string
	}{
		{
			name: "image pull wins over node pressure",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{
									Name: "pull-pod",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "main", Waiting: "ImagePullBackOff"},
									},
								},
							},
						},
					},
				},
				Nodes: clusterhealth.SectionResult[clusterhealth.NodesSection]{
					Data: clusterhealth.NodesSection{
						Nodes: []clusterhealth.NodeInfo{
							{Name: "worker-1", UnhealthyReason: "MemoryPressure: True"},
						},
					},
				},
			},
			wantSubcategory: "image-pull",
			wantErrorCode:   CodeImagePull,
			wantConfidence:  ConfidenceMedium,
		},
		{
			name: "pods section errored, falls through to nodes",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Error: "failed to list pods",
				},
				Nodes: clusterhealth.SectionResult[clusterhealth.NodesSection]{
					Data: clusterhealth.NodesSection{
						Nodes: []clusterhealth.NodeInfo{
							{Name: "worker-1", UnhealthyReason: "DiskPressure: True"},
						},
					},
				},
			},
			wantSubcategory: "node-pressure",
			wantErrorCode:   CodeNodePressure,
			wantConfidence:  ConfidenceHigh,
		},
		{
			name: "specific pattern wins over cluster-distress catch-all",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{
									Name: "pull-pod",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "main", Waiting: "ImagePullBackOff"},
									},
								},
								{
									Name: "other-pod",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "sidecar", Waiting: "RunContainerError"},
									},
								},
							},
						},
					},
				},
			},
			wantSubcategory: "image-pull",
			wantErrorCode:   CodeImagePull,
			wantConfidence:  ConfidenceMedium,
		},
		{
			name: "waiting pattern takes priority over Pending phase",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{
									Name:  "pull-pod",
									Phase: "Pending",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "main", Waiting: "ImagePullBackOff"},
									},
								},
							},
						},
					},
				},
			},
			wantSubcategory: "image-pull",
			wantErrorCode:   CodeImagePull,
			wantConfidence:  ConfidenceMedium,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.report)
			assertClassification(t, got, CategoryInfrastructure, tt.wantSubcategory, tt.wantErrorCode, tt.wantConfidence)
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		patterns []classificationPattern
		want     bool
	}{
		{
			name:     "empty text returns nil",
			text:     "",
			patterns: waitingPatterns,
			want:     false,
		},
		{
			name:     "case-insensitive match",
			text:     "imagepullbackoff: something",
			patterns: waitingPatterns,
			want:     true,
		},
		{
			name:     "no match returns nil",
			text:     "ContainerCreating",
			patterns: waitingPatterns,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPattern(tt.text, tt.patterns)
			if (got != nil) != tt.want {
				t.Errorf("matchesPattern() returned %v, want match=%v", got, tt.want)
			}
		})
	}
}
