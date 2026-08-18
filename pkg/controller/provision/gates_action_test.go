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
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
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
	_ = dsciv2.AddToScheme(s)
	_ = dscv2.AddToScheme(s)
	return s
}

func dsciWithMajor(major uint64) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
		Status: dsciv2.DSCInitializationStatus{
			Release: common.Release{
				Version: ofaversion.OperatorVersion{
					Version: semver.Version{Major: major},
				},
			},
		},
	}
}

func release351() common.Release {
	sv, _ := semver.Parse("3.5.1")
	return common.Release{
		Version: ofaversion.OperatorVersion{Version: sv},
	}
}

func TestCheckUpgradeGates_FreshInstallCreatesEmptyCM(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	conds := &condRecorder{}

	err := provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release351(), conds, nil)

	require.NoError(t, err)
	assert.Empty(t, conds.conditions)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKey{
		Name: gates.AcksConfigMap, Namespace: "test-ns",
	}, cm), "empty CM must be created on fresh install")
	assert.Empty(t, cm.Data)
}

func TestCheckUpgradeGates_SameMajorCreatesEmptyCM(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(dsciWithMajor(3)).Build()
	conds := &condRecorder{}

	err := provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release351(), conds, nil)

	require.NoError(t, err)
	assert.Empty(t, conds.conditions)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKey{
		Name: gates.AcksConfigMap, Namespace: "test-ns",
	}, cm), "empty CM must be created on same-major upgrade")
	assert.Empty(t, cm.Data)
}

func TestCheckUpgradeGates_BlocksOnUpgradeFrom2x(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(dsciWithMajor(2)).Build()
	conds := &condRecorder{}

	err := provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release351(), conds, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unacknowledged upgrade gate(s)")
	assert.Contains(t, err.Error(), "3.5.1")

	require.Len(t, conds.conditions, 1)
	assert.Equal(t, status.AdminAckRequiredReason, conds.conditions[0].Reason)
}

func TestCheckUpgradeGates_AllInTreeGatesAcked(t *testing.T) {
	t.Parallel()

	intreeGates, err := gates.LoadInTreeGates()
	require.NoError(t, err)

	acksData := make(map[string]string, len(intreeGates))
	for k := range intreeGates {
		acksData[k] = "true"
	}

	acksCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: "test-ns"},
		Data:       acksData,
	}

	cli := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(dsciWithMajor(2), acksCM).Build()
	conds := &condRecorder{}

	err = provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release351(), conds, nil)

	require.NoError(t, err)
	assert.Empty(t, conds.conditions)
}

func TestCheckUpgradeGates_MergesClusterAndChartGates(t *testing.T) {
	t.Parallel()

	intreeGates, err := gates.LoadInTreeGates()
	require.NoError(t, err)

	acksData := make(map[string]string, len(intreeGates))
	for k := range intreeGates {
		acksData[k] = "true"
	}

	acksCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: "test-ns"},
		Data:       acksData,
	}

	clusterGateCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gate-from-cluster",
			Namespace: "test-ns",
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			"ack-3.5.1-cluster-extra": "From cluster",
		},
	}

	chartGates := map[string]string{
		"ack-3.5.1-chart-extra": "From chart",
	}

	cli := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(dsciWithMajor(2), acksCM, clusterGateCM).Build()
	conds := &condRecorder{}

	err = provision.CheckUpgradeGatesInNamespace(context.Background(), cli, "test-ns", release351(), conds, chartGates)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unacknowledged upgrade gate(s)")

	updatedAcks := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKey{
		Name: gates.AcksConfigMap, Namespace: "test-ns",
	}, updatedAcks))
	assert.Equal(t, "From cluster", updatedAcks.Data["ack-3.5.1-cluster-extra"])
	assert.Equal(t, "From chart", updatedAcks.Data["ack-3.5.1-chart-extra"])
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
