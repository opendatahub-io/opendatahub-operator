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

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
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
			"sections":              "nodes,pods",
		}, []string{"nodes", "pods"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := server.NewMCPServer("test", "0.0.1")
			registerPlatformHealth(s, cl)
			msg, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": "platform_health", "arguments": tt.args},
			})
			raw, _ := json.Marshal(s.HandleMessage(context.Background(), msg))
			var rpc struct {
				Result struct {
					Content []struct{ Text string } `json:"content"`
					IsError bool                    `json:"isError"`
				} `json:"result"`
			}
			if err := json.Unmarshal(raw, &rpc); err != nil {
				t.Fatalf("unmarshal rpc: %v", err)
			}
			if rpc.Result.IsError {
				t.Fatalf("handler returned error: %s", rpc.Result.Content[0].Text)
			}
			if len(rpc.Result.Content) == 0 {
				t.Fatal("empty content")
			}
			var report clusterhealth.Report
			if err := json.Unmarshal([]byte(rpc.Result.Content[0].Text), &report); err != nil {
				t.Fatalf("unmarshal report: %v", err)
			}
			if report.CollectedAt.IsZero() {
				t.Error("CollectedAt should be non-zero")
			}
			if tt.want != nil {
				if len(report.SectionsRun) != len(tt.want) {
					t.Fatalf("SectionsRun = %v, want %v", report.SectionsRun, tt.want)
				}
				want := make(map[string]bool, len(tt.want))
				for _, s := range tt.want {
					want[s] = true
				}
				for _, s := range report.SectionsRun {
					if !want[s] {
						t.Errorf("unexpected section %q", s)
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
		{"RBAC forbidden", newForbiddenClient(), map[string]any{
			"applications_namespace": "opendatahub",
			"operator_namespace":     "opendatahub-operator-system",
		}, "forbidden"},
		{"CRD not installed", newNoMatchClient(), map[string]any{
			"applications_namespace": "opendatahub",
			"operator_namespace":     "opendatahub-operator-system",
		}, "no matches for kind"},
		{"namespace discovery failed", newForbiddenClient(), map[string]any{}, "namespace discovery failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := server.NewMCPServer("test", "0.0.1")
			registerPlatformHealth(s, tt.client)

			msg, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": "platform_health", "arguments": tt.args},
			})
			raw, _ := json.Marshal(s.HandleMessage(context.Background(), msg))

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

			var report clusterhealth.Report
			if err := json.Unmarshal([]byte(text), &report); err != nil {
				t.Fatalf("unmarshal report: %v", err)
			}

			for _, sec := range []struct{ name, err string }{
				{"nodes", report.Nodes.Error},
				{"pods", report.Pods.Error},
				{"events", report.Events.Error},
				{"operator", report.Operator.Error},
			} {
				if !strings.Contains(sec.err, tt.wantInErr) {
					t.Errorf("section %s: error=%q, want substring %q", sec.name, sec.err, tt.wantInErr)
				}
			}
		})
	}
}
