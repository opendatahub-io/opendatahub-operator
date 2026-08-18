package provision_test

import (
	"context"
	"errors"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
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

func gateCM(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gate-source",
			Namespace: testNS,
			Labels: map[string]string{
				gates.UpgradeGateLabel: "true",
			},
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

// allManaged is a helper that marks all given components as Managed.
func allManaged(names ...string) map[string]operatorv1.ManagementState {
	m := make(map[string]operatorv1.ManagementState, len(names))
	for _, n := range names {
		m[n] = operatorv1.Managed
	}
	return m
}

func runAutoAck(
	t *testing.T,
	reader client.Reader,
	cm *corev1.ConfigMap,
	componentStates map[string]operatorv1.ManagementState,
) error {
	t.Helper()

	return provision.AutoAcknowledgeUpgradeGatesInNamespace(
		t.Context(), reader, cm, testApps, "3.0.0", componentStates,
	)
}

func TestAutoAck_NilConfigMap(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).Build()

	err := runAutoAck(t, cli, nil, nil)

	require.NoError(t, err)
}

func TestAutoAck_EmptyConfigMap(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).Build()
	cm := acksCM(nil)

	err := runAutoAck(t, cli, cm, nil)

	require.NoError(t, err)
}

func TestAutoAck_AllAlreadyAcked(t *testing.T) {
	t.Parallel()

	cm := acksCM(map[string]string{
		"ack-3.0.0-dashboard": "true",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				"ack-3.0.0-dashboard": "Dashboard upgrade",
			}),
		).Build()

	err := runAutoAck(t, cli, cm, allManaged("dashboard"))

	require.NoError(t, err)
	assert.Equal(t, "true", cm.Data["ack-3.0.0-dashboard"])
}

func TestAutoAck_HealthyComponentAutoAcked(t *testing.T) {
	t.Parallel()

	cm := acksCM(map[string]string{
		"ack-3.0.0-dashboard": "Acknowledge upgrade of dashboard from version 2.x to 3.0.0",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				"ack-3.0.0-dashboard": "Dashboard upgrade",
			}),
			readyDeployment(testApps, "dashboard", "dashboard"),
		).Build()

	err := runAutoAck(t, cli, cm, allManaged("dashboard"))

	require.NoError(t, err)
	assert.Equal(t, "true", cm.Data["ack-3.0.0-dashboard"])
}

func TestAutoAck_UnhealthyComponentLeftUnacked(t *testing.T) {
	t.Parallel()

	cm := acksCM(map[string]string{
		"ack-3.0.0-kserve": "Acknowledge upgrade of kserve from version 2.x to 3.0.0",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				"ack-3.0.0-kserve": "KServe upgrade",
			}),
			unreadyDeployment("kserve-controller", "kserve"),
		).Build()

	err := runAutoAck(t, cli, cm, allManaged("kserve"))

	require.NoError(t, err)
	assert.NotEqual(t, "true", cm.Data["ack-3.0.0-kserve"],
		"unhealthy component should remain unacked")
}

func TestAutoAck_ManagedComponentRunsRegisteredCheck(t *testing.T) {
	component := componentApi.TrainerComponentName
	key := "ack-3.0.0-" + component
	cm := acksCM(map[string]string{
		key: "Acknowledge upgrade of trainer",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				key: "Trainer upgrade",
			}),
		).Build()

	provision.RegisterUpgradeCheck(component,
		func(context.Context, client.Reader, string, string) error {
			return errors.New("trainer migration still blocked")
		},
	)

	err := runAutoAck(t, cli, cm, allManaged(component))
	require.NoError(t, err)
	assert.NotEqual(t, "true", cm.Data[key],
		"managed components should remain unacked when their registered check fails")
}

func TestAutoAck_ManagedComponentAutoAcksWhenRegisteredCheckPasses(t *testing.T) {
	component := componentApi.SparkOperatorComponentName
	key := "ack-3.0.0-" + component
	cm := acksCM(map[string]string{
		key: "Acknowledge upgrade of sparkoperator",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				key: "Spark Operator upgrade",
			}),
		).Build()

	provision.RegisterUpgradeCheck(component,
		func(context.Context, client.Reader, string, string) error {
			return nil
		},
	)

	err := runAutoAck(t, cli, cm, allManaged(component))
	require.NoError(t, err)
	assert.Equal(t, "true", cm.Data[key],
		"managed components should auto-ack when their registered check passes")
}

func TestAutoAck_PartialAck(t *testing.T) {
	t.Parallel()

	cm := acksCM(map[string]string{
		"ack-3.0.0-dashboard": "true",
		"ack-3.0.0-kserve":    "Acknowledge upgrade of kserve from version 2.x to 3.0.0",
		"ack-3.0.0-ray":       "Acknowledge upgrade of ray from version 2.x to 3.0.0",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				"ack-3.0.0-dashboard": "Dashboard upgrade",
				"ack-3.0.0-kserve":    "KServe upgrade",
				"ack-3.0.0-ray":       "Ray upgrade",
			}),
			readyDeployment(testApps, "ray-operator", "ray"),
			unreadyDeployment("kserve-controller", "kserve"),
		).Build()

	err := runAutoAck(t, cli, cm, allManaged("dashboard", "kserve", "ray"))

	require.NoError(t, err)
	assert.Equal(t, "true", cm.Data["ack-3.0.0-dashboard"], "already acked stays acked")
	assert.Equal(t, "true", cm.Data["ack-3.0.0-ray"], "healthy component auto-acked")
	assert.NotEqual(t, "true", cm.Data["ack-3.0.0-kserve"], "unhealthy remains unacked")
}

func TestAutoAck_IgnoresOtherVersionKeys(t *testing.T) {
	t.Parallel()

	cm := acksCM(map[string]string{
		"ack-3.0.0-dashboard": "Acknowledge upgrade of dashboard",
		"ack-2.0.0-dashboard": "Old version gate",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				"ack-3.0.0-dashboard": "Dashboard upgrade",
			}),
			readyDeployment(testApps, "dashboard", "dashboard"),
		).Build()

	err := runAutoAck(t, cli, cm, allManaged("dashboard"))

	require.NoError(t, err)
	assert.Equal(t, "true", cm.Data["ack-3.0.0-dashboard"])
	assert.Equal(t, "Old version gate", cm.Data["ack-2.0.0-dashboard"],
		"other version keys should not be touched")
}

func TestAutoAck_NoDeployments_ComponentConsideredHealthy(t *testing.T) {
	t.Parallel()

	cm := acksCM(map[string]string{
		"ack-3.0.0-trustyai": "Acknowledge upgrade of trustyai",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				"ack-3.0.0-trustyai": "TrustyAI upgrade",
			}),
		).Build()

	err := runAutoAck(t, cli, cm, allManaged("trustyai"))

	require.NoError(t, err)
	assert.Equal(t, "true", cm.Data["ack-3.0.0-trustyai"],
		"component with no deployments should be auto-acked")
}

func TestAutoAck_UnmanagedComponentAutoAckedWithoutHealthCheck(t *testing.T) {
	t.Parallel()

	cm := acksCM(map[string]string{
		"ack-3.0.0-dashboard": "Acknowledge upgrade of dashboard",
		"ack-3.0.0-trustyai":  "Acknowledge upgrade of trustyai",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				"ack-3.0.0-dashboard": "Dashboard upgrade",
				"ack-3.0.0-trustyai":  "TrustyAI upgrade",
			}),
			unreadyDeployment("dashboard", "dashboard"),
			unreadyDeployment("trustyai-service", "trustyai"),
		).Build()

	managed := allManaged("dashboard")

	err := runAutoAck(t, cli, cm, managed)

	require.NoError(t, err)
	assert.NotEqual(t, "true", cm.Data["ack-3.0.0-dashboard"],
		"managed component with unready deployments stays unacked")
	assert.Equal(t, "true", cm.Data["ack-3.0.0-trustyai"],
		"unmanaged component should be auto-acked regardless of deployment state")
}

func TestAutoAck_NilManagedMap_StillRunsRegisteredCheck(t *testing.T) {
	key := "ack-3.0.0-dashboard-api-change"
	cm := acksCM(map[string]string{
		key: "Acknowledge dashboard API change",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				key: "Dashboard API change",
			}),
		).Build()

	provision.RegisterUpgradeCheck("dashboard-api-change",
		func(context.Context, client.Reader, string, string) error {
			return errors.New("manual review required")
		},
	)

	err := runAutoAck(t, cli, cm, nil)

	require.NoError(t, err)
	assert.NotEqual(t, "true", cm.Data[key],
		"nil component state map should still run registered checks for non-component keys")
}

func TestAutoAck_UnmanagedComponentBypassesRegisteredCheck(t *testing.T) {
	component := componentApi.CodeFlareComponentName
	key := "ack-3.0.0-" + component
	cm := acksCM(map[string]string{
		key: "Acknowledge upgrade of " + component + " from version 2.x to 3.0.0",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				key: "CodeFlare upgrade",
			}),
		).Build()

	provision.RegisterUpgradeCheck(component,
		func(context.Context, client.Reader, string, string) error {
			return errors.New("legacy resources present")
		},
	)

	err := runAutoAck(t, cli, cm, map[string]operatorv1.ManagementState{
		component: operatorv1.Removed,
	})
	require.NoError(t, err)

	assert.Equal(t, "true", cm.Data[key],
		"known unmanaged components should auto-ack without running their registered check")
}

func TestAutoAck_NonComponentGateStillRunsRegisteredCheck(t *testing.T) {
	key := "ack-3.0.0-dashboard-api-change"
	cm := acksCM(map[string]string{
		key: "Acknowledge dashboard API change",
	})
	cli := fake.NewClientBuilder().WithScheme(autoAckScheme()).
		WithObjects(
			gateCM(map[string]string{
				key: "Dashboard API change",
			}),
		).Build()

	provision.RegisterUpgradeCheck("dashboard-api-change",
		func(context.Context, client.Reader, string, string) error {
			return errors.New("manual review required")
		},
	)

	err := runAutoAck(t, cli, cm, allManaged("dashboard"))
	require.NoError(t, err)

	assert.NotEqual(t, "true", cm.Data[key],
		"gate keys outside the DSC component map should still run their registered check")
}
