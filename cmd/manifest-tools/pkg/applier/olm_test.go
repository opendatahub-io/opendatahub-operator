package applier

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
)

const testConfigYAML = `
components:
  datasciencepipelines:
    odh:
      repo: opendatahub-io/dsp
      ref: main@abc123
      sourcePath: config
imageOverrides:
  RELATED_IMAGE_DSP:
    component: datasciencepipelines
    odh:
      base: quay.io/opendatahub/dsp-operator
      digest: "sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc"
    rhoai:
      base: quay.io/rhoai/dsp-operator
      digest: "sha256:aabbccdd11223344556677889900aabbccddeeff11223344556677889900aabb"
  RELATED_IMAGE_NO_DIGEST:
    component: datasciencepipelines
    odh:
      base: quay.io/opendatahub/no-digest
  RELATED_IMAGE_BAD_DIGEST:
    component: datasciencepipelines
    odh:
      base: quay.io/opendatahub/bad
      digest: "sha256:short"
  BAD_PREFIX:
    component: datasciencepipelines
    odh:
      base: quay.io/opendatahub/bad-prefix
      digest: "sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc"
`

func writeTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(testConfigYAML), 0644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestLoadOverridesFromConfig(t *testing.T) {
	cfgPath := writeTestConfig(t)

	vars, err := loadOverridesFromConfig(cfgPath, "odh")
	if err != nil {
		t.Fatalf("loadOverridesFromConfig failed: %v", err)
	}

	if len(vars) != 1 {
		t.Fatalf("expected 1 var (only DSP has valid prefix+digest+base), got %d: %v", len(vars), vars)
	}

	if vars[0].Name != "RELATED_IMAGE_DSP" {
		t.Errorf("expected RELATED_IMAGE_DSP, got %s", vars[0].Name)
	}
	if vars[0].Value != "quay.io/opendatahub/dsp-operator@sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc" {
		t.Errorf("unexpected value: %s", vars[0].Value)
	}
}

func TestLoadOverridesFromConfig_RHOAI(t *testing.T) {
	cfgPath := writeTestConfig(t)

	vars, err := loadOverridesFromConfig(cfgPath, "rhoai")
	if err != nil {
		t.Fatalf("loadOverridesFromConfig failed: %v", err)
	}

	if len(vars) != 1 {
		t.Fatalf("expected 1 var, got %d", len(vars))
	}

	if vars[0].Value != "quay.io/rhoai/dsp-operator@sha256:aabbccdd11223344556677889900aabbccddeeff11223344556677889900aabb" {
		t.Errorf("unexpected rhoai value: %s", vars[0].Value)
	}
}

func TestLoadOverridesFromConfig_MissingFile(t *testing.T) {
	_, err := loadOverridesFromConfig("/nonexistent/config.yaml", "odh")
	if err == nil {
		t.Error("expected error for missing config")
	}
}

func TestLoadOverridesFromConfig_PlatformNormalization(t *testing.T) {
	cfgPath := writeTestConfig(t)

	vars, err := loadOverridesFromConfig(cfgPath, "OpenDataHub")
	if err != nil {
		t.Fatalf("loadOverridesFromConfig failed: %v", err)
	}

	if len(vars) != 1 || vars[0].Name != "RELATED_IMAGE_DSP" {
		t.Errorf("OpenDataHub should normalize to odh, got %d vars", len(vars))
	}
}

func newFakeSubscription(name, namespace, packageName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "operators.coreos.com/v1alpha1",
			"kind":       "Subscription",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"name": packageName,
			},
		},
	}
}

func newFakeDynClient(objects ...runtime.Object) *fakedynamic.FakeDynamicClient {
	scheme := runtime.NewScheme()
	return fakedynamic.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			subscriptionGVR: "SubscriptionList",
		},
		objects...,
	)
}

func TestFindSubscription_Single(t *testing.T) {
	sub := newFakeSubscription("my-sub", "test-ns", "opendatahub-operator")
	client := newFakeDynClient(sub)

	name, err := findSubscription(context.Background(), client, "test-ns", "opendatahub-operator")
	if err != nil {
		t.Fatalf("findSubscription failed: %v", err)
	}
	if name != "my-sub" {
		t.Errorf("expected my-sub, got %s", name)
	}
}

func TestFindSubscription_None(t *testing.T) {
	client := newFakeDynClient()

	name, err := findSubscription(context.Background(), client, "test-ns", "opendatahub-operator")
	if err != nil {
		t.Fatalf("findSubscription failed: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty, got %s", name)
	}
}

func TestFindSubscription_WrongPackage(t *testing.T) {
	sub := newFakeSubscription("my-sub", "test-ns", "other-operator")
	client := newFakeDynClient(sub)

	name, err := findSubscription(context.Background(), client, "test-ns", "opendatahub-operator")
	if err != nil {
		t.Fatalf("findSubscription failed: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty, got %s", name)
	}
}

func TestFindSubscription_Multiple(t *testing.T) {
	sub1 := newFakeSubscription("sub-1", "test-ns", "opendatahub-operator")
	sub2 := newFakeSubscription("sub-2", "test-ns", "opendatahub-operator")
	client := newFakeDynClient(sub1, sub2)

	_, err := findSubscription(context.Background(), client, "test-ns", "opendatahub-operator")
	if err == nil {
		t.Error("expected error for multiple subscriptions")
	}
}

func int32Ptr(i int32) *int32 { return &i }

func TestFindDeployment(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "opendatahub-operator-controller-manager",
			Namespace: "test-ns",
			Labels:    map[string]string{"control-plane": "controller-manager"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
		},
	}
	client := fakeclientset.NewClientset(deploy)

	name, err := findDeployment(context.Background(), client, "test-ns")
	if err != nil {
		t.Fatalf("findDeployment failed: %v", err)
	}
	if name != "opendatahub-operator-controller-manager" {
		t.Errorf("expected opendatahub-operator-controller-manager, got %s", name)
	}
}

func TestFindDeployment_NoMatch(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-deploy",
			Namespace: "test-ns",
			Labels:    map[string]string{"app": "other"},
		},
	}
	client := fakeclientset.NewClientset(deploy)

	name, err := findDeployment(context.Background(), client, "test-ns")
	if err != nil {
		t.Fatalf("findDeployment failed: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty, got %s", name)
	}
}

func TestWaitForRollout_AlreadyReady(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "manager",
			Namespace:  "test-ns",
			Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
		},
		Status: appsv1.DeploymentStatus{
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
			ObservedGeneration: 1,
		},
	}
	client := fakeclientset.NewClientset(deploy)

	err := waitForRollout(context.Background(), client, "test-ns", "manager", 5*time.Second)
	if err != nil {
		t.Fatalf("waitForRollout failed: %v", err)
	}
}

func TestWaitForRollout_NilReplicas(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "manager",
			Namespace:  "test-ns",
			Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{},
		Status: appsv1.DeploymentStatus{
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
			ObservedGeneration: 1,
		},
	}
	client := fakeclientset.NewClientset(deploy)

	err := waitForRollout(context.Background(), client, "test-ns", "manager", 5*time.Second)
	if err != nil {
		t.Fatalf("waitForRollout should default to 1 replica: %v", err)
	}
}

func TestWaitForRollout_ContextCancelled(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "manager",
			Namespace:  "test-ns",
			Generation: 2,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:      0,
			ObservedGeneration: 1,
		},
	}
	client := fakeclientset.NewClientset(deploy)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForRollout(ctx, client, "test-ns", "manager", 60*time.Second)
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}
