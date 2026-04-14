package main

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
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
