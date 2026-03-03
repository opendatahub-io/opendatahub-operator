package classifier

import (
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
)

func TestClassify(t *testing.T) {
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "image-pull",
			wantErrorCode:   CodeImagePull,
			wantConfidence:  ConfidenceMedium,
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "image-pull",
			wantErrorCode:   CodeImagePull,
			wantConfidence:  ConfidenceMedium,
		},
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "pod-startup",
			wantErrorCode:   CodePodStartup,
			wantConfidence:  ConfidenceMedium,
		},
		{
			name: "OOMKilled in terminated container",
			report: &clusterhealth.Report{
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
			},
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "quota-oom",
			wantErrorCode:   CodeQuotaOOM,
			wantConfidence:  ConfidenceMedium,
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "pod-startup",
			wantErrorCode:   CodePodStartup,
			wantConfidence:  ConfidenceHigh,
		},
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "network",
			wantErrorCode:   CodeNetwork,
			wantConfidence:  ConfidenceMedium,
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "network",
			wantErrorCode:   CodeNetwork,
			wantConfidence:  ConfidenceMedium,
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "storage",
			wantErrorCode:   CodeStorage,
			wantConfidence:  ConfidenceMedium,
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "storage",
			wantErrorCode:   CodeStorage,
			wantConfidence:  ConfidenceMedium,
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "storage",
			wantErrorCode:   CodeStorage,
			wantConfidence:  ConfidenceMedium,
		},
		{
			name: "quota exceeded",
			report: &clusterhealth.Report{
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
			},
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "quota-oom",
			wantErrorCode:   CodeQuotaOOM,
			wantConfidence:  ConfidenceHigh,
		},
		{
			name: "unhealthy node",
			report: &clusterhealth.Report{
				Nodes: clusterhealth.SectionResult[clusterhealth.NodesSection]{
					Data: clusterhealth.NodesSection{
						Nodes: []clusterhealth.NodeInfo{
							{Name: "worker-1", UnhealthyReason: "MemoryPressure: True"},
						},
					},
				},
			},
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "node-pressure",
			wantErrorCode:   CodeNodePressure,
			wantConfidence:  ConfidenceHigh,
		},
		{
			name: "priority: image pull wins over node pressure",
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
			wantCategory:    CategoryInfrastructure,
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "node-pressure",
			wantErrorCode:   CodeNodePressure,
			wantConfidence:  ConfidenceHigh,
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
		{
			name: "unrecognized waiting reason triggers cluster-distress",
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "cluster-distress",
			wantErrorCode:   CodeInfraUnknown,
			wantConfidence:  ConfidenceLow,
		},
		{
			name: "unrecognized terminated reason triggers cluster-distress",
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "cluster-distress",
			wantErrorCode:   CodeInfraUnknown,
			wantConfidence:  ConfidenceLow,
		},
		{
			name: "unready deployment triggers cluster-distress",
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "cluster-distress",
			wantErrorCode:   CodeInfraUnknown,
			wantConfidence:  ConfidenceLow,
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
			wantCategory:    CategoryInfrastructure,
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
			wantCategory:    CategoryInfrastructure,
			wantSubcategory: "image-pull",
			wantErrorCode:   CodeImagePull,
			wantConfidence:  ConfidenceMedium,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.report)
			if got.Category != tt.wantCategory {
				t.Errorf("Category = %q, want %q", got.Category, tt.wantCategory)
			}
			if got.Subcategory != tt.wantSubcategory {
				t.Errorf("Subcategory = %q, want %q", got.Subcategory, tt.wantSubcategory)
			}
			if got.ErrorCode != tt.wantErrorCode {
				t.Errorf("ErrorCode = %d, want %d", got.ErrorCode, tt.wantErrorCode)
			}
			if got.Confidence != tt.wantConfidence {
				t.Errorf("Confidence = %q, want %q", got.Confidence, tt.wantConfidence)
			}
			if len(got.Evidence) == 0 {
				t.Error("Evidence should not be empty")
			}
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
