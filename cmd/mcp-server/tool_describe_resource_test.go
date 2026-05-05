package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// callDescribe registers the describe_resource tool, sends an MCP request, and
// returns the text content and isError flag.
func callDescribe(t *testing.T, cl client.Client, args map[string]any) (string, bool) {
	t.Helper()
	s := server.NewMCPServer("test", "0.0.1")
	registerDescribeResource(s, cl)

	msg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "describe_resource", "arguments": args},
	})
	resp := s.HandleMessage(context.Background(), msg)
	respBytes, _ := json.Marshal(resp)

	// Parse enough of the JSON-RPC response to extract text and isError.
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

func TestDescribeResource(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-config", Namespace: "default",
			Annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": `{"big":"blob"}`,
				"real-annotation": "keep-me",
			},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: "kubectl", Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", FieldsType: "FieldsV1"},
			},
		},
		Data: map[string]string{"key": "value", "db_password": "hunter2"},
	}
	cmSensitiveNS := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-config", Namespace: "openshift-config"},
		Data:       map[string]string{"endpoint": "https://internal:6443"},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data:       map[string][]byte{"password": []byte("s3cret")},
	}
	sa := &corev1.ServiceAccount{
		ObjectMeta:       metav1.ObjectMeta{Name: "my-sa", Namespace: "default"},
		Secrets:          []corev1.ObjectReference{{Name: "my-sa-token"}},
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: "my-pull-secret"}},
	}
	cl := newFakeClient(cm, cmSensitiveNS, ns, secret, sa)

	tests := []struct {
		name        string
		args        map[string]any
		wantError   bool
		wantContain string
		wantAbsent  string
	}{
		{"namespaced configmap", map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "my-config", "namespace": "default"}, false, "ConfigMap", ""},
		{"cluster-scoped namespace", map[string]any{"apiVersion": "v1", "kind": "Namespace", "name": "test-ns"}, false, "Namespace", ""},
		{"strips managedFields", map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "my-config", "namespace": "default"}, false, "", "managedFields"},
		{"strips last-applied", map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "my-config", "namespace": "default"}, false, "real-annotation", "last-applied-configuration"},
		{"not found namespaced", map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "missing", "namespace": "default"}, true, "not found", ""},
		{"not found cluster-scoped", map[string]any{"apiVersion": "v1", "kind": "Namespace", "name": "ghost"}, true, "not found", "in namespace"},
		{"missing name", map[string]any{"apiVersion": "v1", "kind": "ConfigMap"}, true, "required", ""},
		{"missing kind", map[string]any{"apiVersion": "v1", "name": "foo"}, true, "required", ""},
		{"missing apiVersion", map[string]any{"kind": "ConfigMap", "name": "foo"}, true, "required", ""},
		{"all empty", map[string]any{}, true, "required", ""},
		{"secret redacts data", map[string]any{"apiVersion": "v1", "kind": "Secret", "name": "my-secret", "namespace": "default"}, false, "redacted for security", "s3cret"},
		{"configmap redacts sensitive keys", map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "my-config", "namespace": "default"}, false, "[REDACTED]", "hunter2"},
		{"configmap keeps non-sensitive keys", map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "my-config", "namespace": "default"}, false, "\"key\"", ""},
		{"configmap sensitive ns fully redacted", map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "cluster-config", "namespace": "openshift-config"}, false, "security-sensitive namespace", "endpoint"},
		{"serviceaccount redacts secrets", map[string]any{"apiVersion": "v1", "kind": "ServiceAccount", "name": "my-sa", "namespace": "default"}, false, "redacted for security", "my-sa-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, isError := callDescribe(t, cl, tt.args)
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

func TestDescribeResource_ErrorClients(t *testing.T) {
	args := map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": "x", "namespace": "default"}

	tests := []struct {
		name   string
		client client.Client
	}{
		{"nil client", nil},
		{"RBAC forbidden", newForbiddenClient()},
		{"CRD not installed", newNoMatchClient()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, isError := callDescribe(t, tt.client, args)
			if !isError {
				t.Fatalf("expected isError=true, got text: %s", text)
			}
			if text == "" {
				t.Error("expected non-empty error text")
			}
		})
	}
}
