package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
)

// newFakeClient creates a controller-runtime fake client with core + apps schemes.
func newFakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(objs...).Build()
}

// callTool builds a config from args, runs clusterhealth, and verifies JSON round-trip.
func callTool(t *testing.T, cl client.Client, args map[string]interface{}) clusterhealth.Report {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args

	cfg := clusterhealth.Config{
		Client: cl,
		Operator: clusterhealth.OperatorConfig{
			Namespace: stringParam(req, "operator_namespace", getEnvDefault(envOperatorNamespace, defaultOperatorNS)),
			Name:      getEnvDefault(envOperatorDeployment, defaultOperatorDeploy),
		},
		Namespaces: clusterhealth.NamespaceConfig{
			Apps:       stringParam(req, "applications_namespace", getEnvDefault(envApplicationsNamespace, defaultAppsNS)),
			Monitoring: getEnvDefault(envMonitoringNamespace, defaultMonitoringNS),
			Extra:      []string{"kube-system"},
		},
	}
	if s := stringParam(req, "sections", ""); s != "" {
		cfg.OnlySections = splitTrimmed(s)
	} else if l := stringParam(req, "layer", ""); l != "" {
		cfg.Layers = splitTrimmed(l)
	}

	report, err := clusterhealth.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("clusterhealth.Run error: %v", err)
	}

	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("json marshal error: %v", err)
	}
	return *report
}

func TestPlatformHealth(t *testing.T) {
	cl := newFakeClient()

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantSections []string
	}{
		{"default args", nil, nil},
		{"sections filter", map[string]interface{}{"sections": "nodes,pods"}, []string{"nodes", "pods"}},
		{"layer filter", map[string]interface{}{"layer": "infrastructure"}, []string{"nodes", "quotas"}},
		{"sections precedence", map[string]interface{}{"sections": "nodes", "layer": "operator"}, []string{"nodes"}},
		{"custom namespace", map[string]interface{}{"operator_namespace": "custom-ns", "sections": "nodes"}, []string{"nodes"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := callTool(t, cl, tt.args)
			if tt.wantSections == nil {
				return
			}
			if len(report.SectionsRun) != len(tt.wantSections) {
				t.Fatalf("SectionsRun = %v, want %v", report.SectionsRun, tt.wantSections)
			}
			want := make(map[string]bool, len(tt.wantSections))
			for _, s := range tt.wantSections {
				want[s] = true
			}
			for _, s := range report.SectionsRun {
				if !want[s] {
					t.Errorf("unexpected section %q; want %v", s, tt.wantSections)
				}
			}
		})
	}
}

func TestPlatformHealth_NilClient(t *testing.T) {
	_, err := clusterhealth.Run(context.Background(), clusterhealth.Config{})
	if err == nil {
		t.Error("Run(nil client) should return error")
	}
}

func TestSummarizeReport(t *testing.T) {
	for _, tt := range []struct {
		name         string
		sections     string
		wantHealthy  bool
		checkSection string
		wantStatus   string
	}{
		{"healthy nodes only", "nodes", true, "nodes", "ok"},
		{"healthy quotas only", "quotas", true, "quotas", "ok"},
		{"unhealthy operator missing", "operator", false, "operator", "error"},
		{"unhealthy dsc missing", "dsc", false, "dsc", "error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report := callTool(t, newFakeClient(), map[string]interface{}{"sections": tt.sections})
			summary := summarizeReport(&report)

			if summary.Healthy != tt.wantHealthy {
				t.Errorf("Healthy = %v, want %v", summary.Healthy, tt.wantHealthy)
			}
			if summary.CollectedAt.IsZero() {
				t.Error("CollectedAt should not be zero")
			}
			sec, ok := summary.Sections[tt.checkSection]
			if !ok {
				t.Fatalf("missing section %q", tt.checkSection)
			}
			if sec.Status != tt.wantStatus {
				t.Errorf("section %q status = %q, want %q", tt.checkSection, sec.Status, tt.wantStatus)
			}
		})
	}
}

