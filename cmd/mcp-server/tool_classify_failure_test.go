package main

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/failureclassifier"
)

func TestClassifyFailure_ErrorClients(t *testing.T) {
	tests := []struct {
		name         string
		client       client.Client
		wantCategory string
		wantEvidence string
	}{
		{"RBAC forbidden", newForbiddenClient(), "unknown", "no matching classification rule"},
		{"CRD not installed", newNoMatchClient(), "unknown", "no matching classification rule"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := callTool(t, tt.client, nil)
			fc := failureclassifier.Classify(&report)

			if fc.Category != tt.wantCategory {
				t.Errorf("Category = %q, want %q", fc.Category, tt.wantCategory)
			}
			if len(fc.Evidence) == 0 || fc.Evidence[0] != tt.wantEvidence {
				t.Errorf("Evidence = %v, want [%q]", fc.Evidence, tt.wantEvidence)
			}
		})
	}
}

// TestClassifyFailure_FakeClient exercises the full Run + Classify pipeline with a fake client.
func TestClassifyFailure_FakeClient(t *testing.T) {
	cl := newFakeClient()
	report := callTool(t, cl, nil)

	fc := failureclassifier.Classify(&report)

	if fc.Category != "unknown" {
		t.Errorf("Category = %q, want %q", fc.Category, "unknown")
	}
	if fc.ErrorCode != failureclassifier.CodeUnclassifiable {
		t.Errorf("ErrorCode = %d, want %d", fc.ErrorCode, failureclassifier.CodeUnclassifiable)
	}
}

// TestClassifyFailure_MockReports verifies classification with mock report data.
func TestClassifyFailure_MockReports(t *testing.T) {
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
			wantCategory:    "unknown",
			wantSubcategory: "unclassifiable",
			wantErrorCode:   failureclassifier.CodeUnclassifiable,
			wantConfidence:  "low",
		},
		{
			name: "ImagePullBackOff classifies as image-pull",
			report: &clusterhealth.Report{
				Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
					Data: clusterhealth.PodsSection{
						ByNamespace: map[string][]clusterhealth.PodInfo{
							"test-ns": {
								{
									Name: "bad-pod",
									Containers: []clusterhealth.ContainerInfo{
										{Name: "main", Waiting: "ImagePullBackOff: back-off pulling image"},
									},
								},
							},
						},
					},
				},
			},
			wantCategory:    "infrastructure",
			wantSubcategory: "image-pull",
			wantErrorCode:   failureclassifier.CodeImagePull,
			wantConfidence:  "medium",
		},
		{
			name: "node MemoryPressure classifies as node-pressure",
			report: &clusterhealth.Report{
				Nodes: clusterhealth.SectionResult[clusterhealth.NodesSection]{
					Data: clusterhealth.NodesSection{
						Nodes: []clusterhealth.NodeInfo{
							{Name: "worker-1", UnhealthyReason: "MemoryPressure: True"},
						},
					},
				},
			},
			wantCategory:    "infrastructure",
			wantSubcategory: "node-pressure",
			wantErrorCode:   failureclassifier.CodeNodePressure,
			wantConfidence:  "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := failureclassifier.Classify(tt.report)

			if fc.Category != tt.wantCategory {
				t.Errorf("Category = %q, want %q", fc.Category, tt.wantCategory)
			}
			if fc.Subcategory != tt.wantSubcategory {
				t.Errorf("Subcategory = %q, want %q", fc.Subcategory, tt.wantSubcategory)
			}
			if fc.ErrorCode != tt.wantErrorCode {
				t.Errorf("ErrorCode = %d, want %d", fc.ErrorCode, tt.wantErrorCode)
			}
			if fc.Confidence != tt.wantConfidence {
				t.Errorf("Confidence = %q, want %q", fc.Confidence, tt.wantConfidence)
			}
			if len(fc.Evidence) == 0 {
				t.Error("Evidence should not be empty")
			}
		})
	}
}
