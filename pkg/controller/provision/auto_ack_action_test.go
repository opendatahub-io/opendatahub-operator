package provision_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
)

const (
	testNS   = "test-ns"
	testApps = "apps-ns"
)

func autoAckScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	return s
}

func acksCM(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gates.AcksConfigMap,
			Namespace: testNS,
		},
		Data: data,
	}
}

func readyDeployment(ns, name, component string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				"app.opendatahub.io/" + component: "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
		},
	}
}

func unreadyDeployment(name, component string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testApps,
			Labels: map[string]string{
				"app.opendatahub.io/" + component: "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
		},
	}
}

func allManaged(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func TestAutoAck_NoAcksConfigMap(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).Build()

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0", nil)

	require.NoError(t, err)
}

func TestAutoAck_AllAlreadyAcked(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			acksCM(map[string]string{
				"ack-3.0.0-dashboard": "true",
			}),
		).Build()

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0",
		allManaged("dashboard"))

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))
	assert.Equal(t, "true", cm.Data["ack-3.0.0-dashboard"])
}

func TestAutoAck_HealthyComponentAutoAcked(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			acksCM(map[string]string{
				"ack-3.0.0-dashboard": "Acknowledge upgrade of dashboard from version 2.x to 3.0.0",
			}),
			readyDeployment(testApps, "dashboard", "dashboard"),
		).Build()

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0",
		allManaged("dashboard"))

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))
	assert.Equal(t, "true", cm.Data["ack-3.0.0-dashboard"])
}

func TestAutoAck_UnhealthyComponentLeftUnacked(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			acksCM(map[string]string{
				"ack-3.0.0-kserve": "Acknowledge upgrade of kserve from version 2.x to 3.0.0",
			}),
			unreadyDeployment("kserve-controller", "kserve"),
		).Build()

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0",
		allManaged("kserve"))

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))
	assert.NotEqual(t, "true", cm.Data["ack-3.0.0-kserve"],
		"unhealthy component should remain unacked")
}

func TestAutoAck_PartialAck(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			acksCM(map[string]string{
				"ack-3.0.0-dashboard": "true",
				"ack-3.0.0-kserve":    "Acknowledge upgrade of kserve from version 2.x to 3.0.0",
				"ack-3.0.0-ray":       "Acknowledge upgrade of ray from version 2.x to 3.0.0",
			}),
			readyDeployment(testApps, "ray-operator", "ray"),
			unreadyDeployment("kserve-controller", "kserve"),
		).Build()

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0",
		allManaged("dashboard", "kserve", "ray"))

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))

	assert.Equal(t, "true", cm.Data["ack-3.0.0-dashboard"], "already acked stays acked")
	assert.Equal(t, "true", cm.Data["ack-3.0.0-ray"], "healthy component auto-acked")
	assert.NotEqual(t, "true", cm.Data["ack-3.0.0-kserve"], "unhealthy remains unacked")
}

func TestAutoAck_IgnoresOtherVersionKeys(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			acksCM(map[string]string{
				"ack-3.0.0-dashboard": "Acknowledge upgrade of dashboard",
				"ack-2.0.0-dashboard": "Old version gate",
			}),
			readyDeployment(testApps, "dashboard", "dashboard"),
		).Build()

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0",
		allManaged("dashboard"))

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))

	assert.Equal(t, "true", cm.Data["ack-3.0.0-dashboard"])
	assert.Equal(t, "Old version gate", cm.Data["ack-2.0.0-dashboard"],
		"other version keys should not be touched")
}

func TestAutoAck_NoDeployments_ComponentConsideredHealthy(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			acksCM(map[string]string{
				"ack-3.0.0-trustyai": "Acknowledge upgrade of trustyai",
			}),
		).Build()

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0",
		allManaged("trustyai"))

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))
	assert.Equal(t, "true", cm.Data["ack-3.0.0-trustyai"],
		"component with no deployments should be auto-acked")
}

func TestAutoAck_UnmanagedComponentAutoAckedWithoutHealthCheck(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			acksCM(map[string]string{
				"ack-3.0.0-dashboard": "Acknowledge upgrade of dashboard",
				"ack-3.0.0-trustyai":  "Acknowledge upgrade of trustyai",
			}),
			unreadyDeployment("dashboard", "dashboard"),
			unreadyDeployment("trustyai-service", "trustyai"),
		).Build()

	managed := allManaged("dashboard")

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0", managed)

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))

	assert.NotEqual(t, "true", cm.Data["ack-3.0.0-dashboard"],
		"managed component with unready deployments stays unacked")
	assert.Equal(t, "true", cm.Data["ack-3.0.0-trustyai"],
		"unmanaged component should be auto-acked regardless of deployment state")
}

func TestAutoAck_NilManagedMap_AllComponentsAutoAcked(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			acksCM(map[string]string{
				"ack-3.0.0-kserve": "Acknowledge upgrade of kserve",
			}),
			unreadyDeployment("kserve-controller", "kserve"),
		).Build()

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0", nil)

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))
	assert.Equal(t, "true", cm.Data["ack-3.0.0-kserve"],
		"nil managed map (xKS) means all components are treated as unmanaged and auto-acked")
}

func TestAutoAck_UnmanagedProblematicComponentStillChecked(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				"ack-3.0.0-modelmeshserving": "ModelMeshServing upgrade",
			}),
			acksCM(map[string]string{
				"ack-3.0.0-modelmeshserving": "Acknowledge upgrade of modelmeshserving from version 2.x to 3.0.0",
			}),
		).Build()

	provision.RegisterUpgradeCheck(componentApi.ModelMeshServingComponentName,
		func(context.Context, client.Reader, string, string) error {
			return errors.New("legacy ModelMesh resources present")
		},
	)

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0", map[string]bool{})

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))
	assert.NotEqual(t, "true", cm.Data["ack-3.0.0-modelmeshserving"],
		"problematic unmanaged component should still run its gate check")
}

func TestAutoAck_UnmanagedProblematicComponentAutoAckedWhenCheckPasses(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				"ack-3.0.0-modelmeshserving": "ModelMeshServing upgrade",
			}),
			acksCM(map[string]string{
				"ack-3.0.0-modelmeshserving": "Acknowledge upgrade of modelmeshserving from version 2.x to 3.0.0",
			}),
		).Build()

	provision.RegisterUpgradeCheck(componentApi.ModelMeshServingComponentName,
		func(context.Context, client.Reader, string, string) error {
			return nil
		},
	)

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0", map[string]bool{})

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))
	assert.Equal(t, "true", cm.Data["ack-3.0.0-modelmeshserving"],
		"problematic unmanaged component should still auto-ack when its gate check passes")
}

func TestAutoAck_UnmanagedCodeFlareStillChecked(t *testing.T) {
	t.Parallel()

	codeFlareComponentName := strings.ToLower(componentApi.CodeFlareKind)

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			acksCM(map[string]string{
				"ack-3.0.0-codeflare": "Acknowledge upgrade of codeflare from version 2.x to 3.0.0",
			}),
		).Build()

	provision.RegisterUpgradeCheck(codeFlareComponentName,
		func(context.Context, client.Reader, string, string) error {
			return errors.New("legacy CodeFlare resources present")
		},
	)

	err := provision.AutoAcknowledgeUpgradeGatesInNamespace(
		context.Background(), cli, cli, testNS, testApps, "3.0.0", map[string]bool{})

	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(),
		client.ObjectKey{Name: gates.AcksConfigMap, Namespace: testNS}, cm))
	assert.NotEqual(t, "true", cm.Data["ack-3.0.0-codeflare"],
		"problematic unmanaged codeflare component should still run its gate check")
}
