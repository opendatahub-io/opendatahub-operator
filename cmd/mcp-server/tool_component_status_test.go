package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
)

func TestComponentStatus(t *testing.T) {
	cl := newFakeClient()

	t.Run("unknown component", func(t *testing.T) {
		_, err := clusterhealth.GetComponentStatus(context.Background(), cl, "bogus", defaultAppsNS)
		if err == nil {
			t.Error("expected error for unknown component")
		}
	})

	t.Run("no CR exists", func(t *testing.T) {
		r, err := clusterhealth.GetComponentStatus(context.Background(), cl, "kserve", defaultAppsNS)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.CRFound {
			t.Error("expected crFound=false")
		}
	})
}

func TestComponentStatus_DeploymentsAndPods(t *testing.T) {
	replicas := int32(2)
	cl := newFakeClient(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "kserve-ctrl", Namespace: defaultAppsNS,
				Labels: map[string]string{"app.opendatahub.io/kserve": "true"},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "kserve"}},
			},
			Status: appsv1.DeploymentStatus{ReadyReplicas: 2},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "kserve-pod", Namespace: defaultAppsNS,
				Labels: map[string]string{"app.opendatahub.io/kserve": "true"},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)

	r, err := clusterhealth.GetComponentStatus(context.Background(), cl, "kserve", defaultAppsNS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Deployments) != 1 || r.Deployments[0].Ready != 2 || r.Deployments[0].Replicas != 2 {
		t.Errorf("Deployments = %+v, want 1 with ready=2", r.Deployments)
	}
	if len(r.Pods) != 1 || r.Pods[0].Phase != "Running" {
		t.Errorf("Pods = %+v, want 1 Running", r.Pods)
	}
}

func TestFetchManagedResources(t *testing.T) {
	callTool := func(t *testing.T, cl client.Client, component string) string {
		t.Helper()
		s := server.NewMCPServer("test", "0.0.1")
		registerComponentStatus(s, cl)
		msg, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": "component_status", "arguments": map[string]any{"component": component}},
		})
		respBytes, _ := json.Marshal(s.HandleMessage(context.Background(), msg))
		var rpcResp struct {
			Result struct {
				Content []struct{ Text string } `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
			t.Fatalf("unmarshal rpc response: %v", err)
		}
		return rpcResp.Result.Content[0].Text
	}

	t.Run("no resources in empty cluster", func(t *testing.T) {
		resources, err := fetchManagedResources(context.Background(), newFakeClient(), "dashboard", defaultAppsNS)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resources) != 0 {
			t.Fatalf("got %d resources, want 0", len(resources))
		}
	})

	t.Run("tool response includes managedResources", func(t *testing.T) {
		kserveLabel := map[string]string{"app.opendatahub.io/kserve": "true"}
		replicas := int32(1)
		cl := newFakeClient(
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "kserve-ctrl", Namespace: defaultAppsNS, Labels: kserveLabel},
				Spec:       appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "kserve"}}},
				Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
			},
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "webhook-svc", Namespace: defaultAppsNS, Labels: kserveLabel}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "isvc-config", Namespace: defaultAppsNS, Labels: kserveLabel}},
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "ray-svc", Namespace: defaultAppsNS, Labels: map[string]string{"app.opendatahub.io/ray": "true"}}},
		)

		var got struct {
			Deployments      []json.RawMessage `json:"deployments"`
			ManagedResources []ManagedResource `json:"managedResources"`
		}
		if err := json.Unmarshal([]byte(callTool(t, cl, "kserve")), &got); err != nil {
			t.Fatalf("unmarshal tool output: %v", err)
		}
		if len(got.Deployments) != 1 {
			t.Errorf("deployments = %d, want 1", len(got.Deployments))
		}
		wantKinds := []string{"Service", "ConfigMap"}
		if len(got.ManagedResources) != len(wantKinds) {
			t.Fatalf("managedResources = %d, want %d", len(got.ManagedResources), len(wantKinds))
		}
		for i, want := range wantKinds {
			if got.ManagedResources[i].Kind != want {
				t.Errorf("managedResources[%d].Kind = %q, want %q", i, got.ManagedResources[i].Kind, want)
			}
		}
	})
}
