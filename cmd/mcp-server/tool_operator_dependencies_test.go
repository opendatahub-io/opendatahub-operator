package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
)

func makeDeployment(ns, name string, ready, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: name, Image: "quay.io/" + name + ":v1.0.0"},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: ready, Replicas: replicas},
	}
}

func makePod(ns, name, phase string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Status:     corev1.PodStatus{Phase: corev1.PodPhase(phase)},
	}
}

// callDeps registers the operator_dependencies tool, sends an MCP request, and
// returns the text content and isError flag.
func callDeps(t *testing.T, cl client.Client, args map[string]any) (string, bool) {
	t.Helper()
	s := server.NewMCPServer("test", "0.0.1")
	registerOperatorDependencies(s, cl)

	msg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "operator_dependencies", "arguments": args},
	})
	resp := s.HandleMessage(context.Background(), msg)
	respBytes, _ := json.Marshal(resp)

	var rpcResp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(rpcResp.Result.Content) == 0 {
		t.Fatal("empty content in response")
	}
	return rpcResp.Result.Content[0].Text, rpcResp.Result.IsError
}

func TestOperatorDependencies(t *testing.T) {
	cl := newFakeClient(
		makeDeployment(defaultOperatorNS, defaultOperatorDeploy, 1, 1),
		makePod(defaultOperatorNS, "op-pod", "Running", map[string]string{"app": defaultOperatorDeploy}),
		makeDeployment("cert-manager-operator", "cert-manager-operator", 1, 1),
		makePod("cert-manager-operator", "cm-pod", "Running", map[string]string{"app": "cert-manager-operator"}),
		makeDeployment("openshift-tempo-operator", "tempo-operator", 0, 1),
		makePod("openshift-tempo-operator", "tempo-pod", "Pending", map[string]string{"app": "tempo-operator"}),
	)

	report, err := clusterhealth.Run(context.Background(), clusterhealth.Config{
		Client:       cl,
		Operator:     clusterhealth.OperatorConfig{Namespace: defaultOperatorNS, Name: defaultOperatorDeploy},
		OnlySections: []string{"operator"},
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	deps := report.Operator.Data.DependentOperators
	if len(deps) != clusterhealth.DependentOperatorCount() {
		t.Fatalf("got %d dependents, want %d", len(deps), clusterhealth.DependentOperatorCount())
	}

	tests := []struct {
		name          string
		filterName    string
		wantInstalled bool
		wantError     bool
		wantFound     bool
	}{
		{"cert-manager", "", true, false, true},
		{"tempo", "", true, true, true},
		{"kueue", "", false, false, true},
		{"opentelemetry", "", false, false, true},
		{"jobset", "", false, false, true},
		{"leader-worker-set", "", false, false, true},
		{"cluster-observability", "", false, false, true},
		{"kuadrant", "", false, false, true},
		{"filter cert-manager by name", "cert-manager", true, false, true},
		{"filter tempo by name", "tempo", true, true, true},
		{"filter unknown name", "nonexistent", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookupName := tt.name
			if tt.filterName != "" {
				lookupName = tt.filterName
			}

			var dep *clusterhealth.DependentOperatorResult
			for i := range deps {
				if deps[i].Name == lookupName {
					dep = &deps[i]
				}
			}

			if !tt.wantFound {
				if dep != nil {
					t.Fatalf("expected %q to not be found, but it was", lookupName)
				}
				return
			}
			if dep == nil {
				t.Fatalf("%s not found in results", lookupName)
			}
			if dep.Installed != tt.wantInstalled {
				t.Errorf("Installed = %v, want %v", dep.Installed, tt.wantInstalled)
			}
			if (dep.Error != "") != tt.wantError {
				t.Errorf("Error = %q, wantError = %v", dep.Error, tt.wantError)
			}
			if dep.Installed && dep.ImageRef == "" {
				t.Error("installed operator should have version info")
			}
		})
	}

	if _, err := json.Marshal(deps); err != nil {
		t.Fatalf("json marshal: %v", err)
	}

	healthyCl := newFakeClient(
		makeDeployment(defaultOperatorNS, defaultOperatorDeploy, 1, 1),
		makePod(defaultOperatorNS, "op-pod", "Running", map[string]string{"app": defaultOperatorDeploy}),
		makeDeployment("cert-manager-operator", "cert-manager-operator", 1, 1),
		makePod("cert-manager-operator", "cm-pod", "Running", map[string]string{"app": "cert-manager-operator"}),
		makeDeployment("openshift-tempo-operator", "tempo-operator", 1, 1),
		makePod("openshift-tempo-operator", "tempo-pod", "Running", map[string]string{"app": "tempo-operator"}),
	)

	mcpTests := []struct {
		name        string
		client      client.Client
		args        map[string]any
		wantError   bool
		wantContain string
		wantAbsent  string
	}{
		{"mcp all deps", healthyCl, map[string]any{}, false, "cert-manager", ""},
		{"mcp filter by name", healthyCl, map[string]any{"name": "cert-manager"}, false, "cert-manager", "tempo"},
		{"mcp unknown name", healthyCl, map[string]any{"name": "nonexistent"}, true, "not found", ""},
		{"mcp operator section warning", cl, map[string]any{}, false, "warning:", ""},
	}

	for _, tt := range mcpTests {
		t.Run(tt.name, func(t *testing.T) {
			text, isError := callDeps(t, tt.client, tt.args)
			if isError != tt.wantError {
				t.Fatalf("IsError = %v, want %v; text: %s", isError, tt.wantError, text)
			}
			if tt.wantContain != "" && !strings.Contains(text, tt.wantContain) {
				t.Errorf("response missing %q", tt.wantContain)
			}
			if tt.wantAbsent != "" && strings.Contains(text, tt.wantAbsent) {
				t.Errorf("response should not contain %q", tt.wantAbsent)
			}
		})
	}
}

func TestOperatorDependencies_NilClient(t *testing.T) {
	_, isError := callDeps(t, nil, map[string]any{})
	if !isError {
		t.Error("operator_dependencies with nil client should return error")
	}
}
