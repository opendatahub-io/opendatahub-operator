package provision_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
)

func checkScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	return s
}

func TestDefaultUpgradeCheck_AllReady(t *testing.T) {
	t.Parallel()

	replicas := int32(2)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dashboard",
			Namespace: "apps-ns",
			Labels:    map[string]string{"app.opendatahub.io/dashboard": "true"},
		},
		Spec:   appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 2},
	}

	cli := fake.NewClientBuilder().WithScheme(checkScheme()).WithObjects(dep).Build()

	err := provision.DefaultUpgradeCheck(context.Background(), cli, "dashboard", "apps-ns")
	require.NoError(t, err)
}

func TestDefaultUpgradeCheck_NotReady(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kserve-controller",
			Namespace: "apps-ns",
			Labels:    map[string]string{"app.opendatahub.io/kserve": "true"},
		},
		Spec:   appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1},
	}

	cli := fake.NewClientBuilder().WithScheme(checkScheme()).WithObjects(dep).Build()

	err := provision.DefaultUpgradeCheck(context.Background(), cli, "kserve", "apps-ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready")
	assert.Contains(t, err.Error(), "1/3")
}

func TestDefaultUpgradeCheck_NoDeployments(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(checkScheme()).Build()

	err := provision.DefaultUpgradeCheck(context.Background(), cli, "trustyai", "apps-ns")
	require.NoError(t, err)
}

func TestDefaultUpgradeCheck_MultipleDeployments(t *testing.T) {
	t.Parallel()

	replicas := int32(1)
	dep1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kserve-controller",
			Namespace: "apps-ns",
			Labels:    map[string]string{"app.opendatahub.io/kserve": "true"},
		},
		Spec:   appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1},
	}
	dep2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kserve-webhook",
			Namespace: "apps-ns",
			Labels:    map[string]string{"app.opendatahub.io/kserve": "true"},
		},
		Spec:   appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 0},
	}

	cli := fake.NewClientBuilder().WithScheme(checkScheme()).
		WithObjects(dep1, dep2).Build()

	err := provision.DefaultUpgradeCheck(context.Background(), cli, "kserve", "apps-ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kserve-webhook")
}

func TestGetUpgradeCheck_DefaultFallback(t *testing.T) {
	t.Parallel()

	fn := provision.GetUpgradeCheck("nonexistent-component")
	assert.NotNil(t, fn)

	cli := fake.NewClientBuilder().WithScheme(checkScheme()).Build()
	err := fn(context.Background(), cli, "nonexistent-component", "apps-ns")
	require.NoError(t, err)
}

func TestRegisterUpgradeCheck_CustomOverridesDefault(t *testing.T) {
	t.Parallel()

	called := false
	provision.RegisterUpgradeCheck("test-custom-component", func(ctx context.Context, reader client.Reader, component, namespace string) error {
		called = true
		return nil
	})

	fn := provision.GetUpgradeCheck("test-custom-component")
	cli := fake.NewClientBuilder().WithScheme(checkScheme()).Build()
	err := fn(context.Background(), cli, "test-custom-component", "apps-ns")

	require.NoError(t, err)
	assert.True(t, called, "custom check should have been called")
}
