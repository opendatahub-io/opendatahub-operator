package provision_test

import (
	"context"
	"testing"

	"github.com/blang/semver/v4"
	ofaversion "github.com/operator-framework/api/pkg/lib/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type condRecorder struct {
	conditions []common.Condition
}

func (c *condRecorder) SetCondition(cond common.Condition) {
	c.conditions = append(c.conditions, cond)
}

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return s
}

func release(ver string) common.Release {
	sv, _ := semver.Parse(ver)
	return common.Release{
		Version: ofaversion.OperatorVersion{Version: sv},
	}
}

func TestCheckUpgradeGates_NoGates(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	conds := &condRecorder{}

	err := provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release("2.0.0"), conds, nil)

	require.NoError(t, err)
	assert.Empty(t, conds.conditions)
}

func TestCheckUpgradeGates_AllGatesAcked(t *testing.T) {
	t.Parallel()

	acksCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: "test-ns"},
		Data: map[string]string{
			"ack-2.0.0-breaking-change": "true",
		},
	}

	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gate-source",
			Namespace: "test-ns",
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			"ack-2.0.0-breaking-change": "Review the migration guide before proceeding",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(acksCM, source).Build()
	conds := &condRecorder{}

	err := provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release("2.0.0"), conds, nil)

	require.NoError(t, err)
	assert.Empty(t, conds.conditions)
}

func TestCheckUpgradeGates_UnackedGatesBlockProvisioning(t *testing.T) {
	t.Parallel()

	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gate-source",
			Namespace: "test-ns",
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			"ack-2.0.0-kserve-api-change": "KServe API changed; review migration guide",
			"ack-2.0.0-model-registry":    "Model Registry schema updated",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(source).Build()
	conds := &condRecorder{}

	err := provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release("2.0.0"), conds, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 unacknowledged upgrade gate(s)")
	assert.Contains(t, err.Error(), "2.0.0")

	require.Len(t, conds.conditions, 1)
	cond := conds.conditions[0]
	assert.Equal(t, status.ConditionTypeProvisioningProgress, cond.Type)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, status.AdminAckRequiredReason, cond.Reason)
	assert.Contains(t, cond.Message, "ack-2.0.0-kserve-api-change")
	assert.Contains(t, cond.Message, "ack-2.0.0-model-registry")

	acksCM := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKey{
		Name: gates.AcksConfigMap, Namespace: "test-ns",
	}, acksCM))
	assert.Equal(t, "KServe API changed; review migration guide", acksCM.Data["ack-2.0.0-kserve-api-change"])
	assert.Equal(t, "Model Registry schema updated", acksCM.Data["ack-2.0.0-model-registry"])
}

func TestCheckUpgradeGates_IgnoresOtherVersions(t *testing.T) {
	t.Parallel()

	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gate-source",
			Namespace: "test-ns",
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			"ack-1.9.0-old-gate": "This is from a previous version",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(source).Build()
	conds := &condRecorder{}

	err := provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release("3.0.0"), conds, nil)

	require.NoError(t, err)
	assert.Empty(t, conds.conditions)
}

func TestCheckUpgradeGates_WritesDescriptionsToAcks(t *testing.T) {
	t.Parallel()

	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gate-source",
			Namespace: "test-ns",
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			"ack-2.0.0-discovered-gate": "Discovered gate message",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(source).Build()
	conds := &condRecorder{}

	err := provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release("2.0.0"), conds, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 unacknowledged upgrade gate(s)")

	acksCM := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKey{
		Name: gates.AcksConfigMap, Namespace: "test-ns",
	}, acksCM))
	assert.Equal(t, "Discovered gate message", acksCM.Data["ack-2.0.0-discovered-gate"])
}

func TestCheckUpgradeGates_MergesChartGates(t *testing.T) {
	t.Parallel()

	clusterGateCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gate-from-cluster",
			Namespace: "test-ns",
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			"ack-2.0.0-cluster-gate": "From cluster",
		},
	}

	chartGates := map[string]string{
		"ack-2.0.0-chart-gate": "From chart",
	}

	cli := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(clusterGateCM).Build()
	conds := &condRecorder{}

	err := provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release("2.0.0"), conds, chartGates)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 unacknowledged upgrade gate(s)")

	acksCM := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKey{
		Name: gates.AcksConfigMap, Namespace: "test-ns",
	}, acksCM))
	assert.Equal(t, "From cluster", acksCM.Data["ack-2.0.0-cluster-gate"])
	assert.Equal(t, "From chart", acksCM.Data["ack-2.0.0-chart-gate"])
}

func TestExtractUpgradeGates_StashesOnGateEntries(t *testing.T) {
	t.Parallel()

	gateCM := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "module-gate",
				"namespace": "test-ns",
				"labels": map[string]any{
					gates.UpgradeGateLabel: "true",
				},
			},
			"data": map[string]any{
				"ack-2.0.0-module-gate": "Module gate",
			},
		},
	}

	regularCM := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "regular-config",
				"namespace": "test-ns",
			},
			"data": map[string]any{
				"key": "value",
			},
		},
	}

	deployment := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      "my-deploy",
				"namespace": "test-ns",
			},
		},
	}

	rr := &odhtype.ReconciliationRequest{
		Resources: []unstructured.Unstructured{gateCM, regularCM, deployment},
	}

	err := provision.ExtractUpgradeGates(context.Background(), rr)
	require.NoError(t, err)

	assert.Len(t, rr.Resources, 2, "gate CM should be removed, 2 resources remain")
	assert.Equal(t, "regular-config", rr.Resources[0].GetName())
	assert.Equal(t, "my-deploy", rr.Resources[1].GetName())

	require.NotNil(t, rr.GateEntries)
	assert.Equal(t, "Module gate", rr.GateEntries["ack-2.0.0-module-gate"])
}

func TestExtractUpgradeGates_NoGateCMs(t *testing.T) {
	t.Parallel()

	regular := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "regular",
				"namespace": "test-ns",
			},
		},
	}

	rr := &odhtype.ReconciliationRequest{
		Resources: []unstructured.Unstructured{regular},
	}

	err := provision.ExtractUpgradeGates(context.Background(), rr)
	require.NoError(t, err)

	assert.Len(t, rr.Resources, 1, "no resources should be removed")
	assert.Nil(t, rr.GateEntries)
}
