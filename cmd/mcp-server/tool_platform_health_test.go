package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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

func TestPlatformHealth_HandlerHappyPath(t *testing.T) {
	cl := newFakeClient()
	tests := []struct {
		name string
		args map[string]any
		want []string
	}{
		{"empty arguments", map[string]any{}, nil},
		{"explicit namespace overrides", map[string]any{
			"operator_namespace":     "custom-op-ns",
			"applications_namespace": "custom-apps-ns",
			"sections":               "nodes,pods",
		}, []string{"nodes", "pods"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := server.NewMCPServer("test", "0.0.1")
			registerPlatformHealth(s, cl)
			msg, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": "platform_health", "arguments": tt.args},
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			raw, err := json.Marshal(s.HandleMessage(context.Background(), msg))
			if err != nil {
				t.Fatalf("marshal handler response: %v", err)
			}
			var rpc struct {
				Result struct {
					Content []struct{ Text string } `json:"content"`
					IsError bool                    `json:"isError"`
				} `json:"result"`
			}
			if err := json.Unmarshal(raw, &rpc); err != nil {
				t.Fatalf("unmarshal rpc: %v", err)
			}
			if len(rpc.Result.Content) == 0 {
				t.Fatal("empty content")
			}
			if rpc.Result.IsError {
				t.Fatalf("handler returned error: %s", rpc.Result.Content[0].Text)
			}
			var summary HealthSummary
			if err := json.Unmarshal([]byte(rpc.Result.Content[0].Text), &summary); err != nil {
				t.Fatalf("unmarshal summary: %v", err)
			}
			if summary.CollectedAt.IsZero() {
				t.Error("CollectedAt should be non-zero")
			}
			if tt.want != nil {
				if len(summary.Sections) != len(tt.want) {
					t.Fatalf("Sections = %v, want %v", summary.Sections, tt.want)
				}
				for _, s := range tt.want {
					if _, ok := summary.Sections[s]; !ok {
						t.Errorf("missing section %q", s)
					}
				}
			}
		})
	}
}

func TestPlatformHealth_ErrorClients(t *testing.T) {
	tests := []struct {
		name      string
		client    client.Client
		args      map[string]any
		wantInErr string
	}{
		{"namespace discovery RBAC", newForbiddenClient(), map[string]any{}, "RBAC insufficient"},
		{"namespace discovery CRD", newNoMatchClient(), map[string]any{}, "CRD not installed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := server.NewMCPServer("test", "0.0.1")
			registerPlatformHealth(s, tt.client)

			msg, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": "platform_health", "arguments": tt.args},
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			raw, err := json.Marshal(s.HandleMessage(context.Background(), msg))
			if err != nil {
				t.Fatalf("marshal handler response: %v", err)
			}

			var rpc struct {
				Result struct {
					Content []struct{ Text string } `json:"content"`
					IsError bool                    `json:"isError"`
				} `json:"result"`
			}
			if err := json.Unmarshal(raw, &rpc); err != nil {
				t.Fatalf("unmarshal rpc: %v", err)
			}
			if len(rpc.Result.Content) == 0 {
				t.Fatal("empty content")
			}

			text := rpc.Result.Content[0].Text

			if rpc.Result.IsError {
				if !strings.Contains(text, tt.wantInErr) {
					t.Errorf("error text=%q, want substring %q", text, tt.wantInErr)
				}
				return
			}

			var summary HealthSummary
			if err := json.Unmarshal([]byte(text), &summary); err != nil {
				t.Fatalf("unmarshal summary: %v", err)
			}

			found := false
			for _, sec := range summary.Sections {
				if strings.Contains(sec.Error, tt.wantInErr) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no section contains error substring %q", tt.wantInErr)
			}
		})
	}
}
