package gates_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
)

func TestEnsureGates_CreatesConfigMapWithDescriptions(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), map[string]string{
		"ack-2.0.0-api-change": "API changed; review migration guide",
	}, "2.0.0")

	require.NoError(t, err)
	require.Len(t, unacked, 1)
	assert.Equal(t, "ack-2.0.0-api-change", unacked[0].Key)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), acksObjectKey(), cm))
	assert.Equal(t, "API changed; review migration guide", cm.Data["ack-2.0.0-api-change"])
}

func TestEnsureGates_PreservesAckedEntries(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: testNamespace},
		Data: map[string]string{
			"ack-2.0.0-api-change":        "true",
			"ack-2.0.0-storage-migration": "Back up data before proceeding",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), map[string]string{
		"ack-2.0.0-api-change":        "API changed; review migration guide",
		"ack-2.0.0-storage-migration": "Back up data before proceeding",
	}, "2.0.0")

	require.NoError(t, err)
	require.Len(t, unacked, 1)
	assert.Equal(t, "ack-2.0.0-storage-migration", unacked[0].Key)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), acksObjectKey(), cm))
	assert.Equal(t, "true", cm.Data["ack-2.0.0-api-change"], "acked entry must not be overwritten")
}

func TestEnsureGates_AllAcked(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: testNamespace},
		Data: map[string]string{
			"ack-2.0.0-api-change": "true",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), map[string]string{
		"ack-2.0.0-api-change": "API changed",
	}, "2.0.0")

	require.NoError(t, err)
	assert.Empty(t, unacked)
}

func TestEnsureGates_FiltersToVersionPrefix(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), map[string]string{
		"ack-1.0.0-old-gate":     "Old gate",
		"ack-2.0.0-current-gate": "Current gate",
	}, "2.0.0")

	require.NoError(t, err)
	require.Len(t, unacked, 1)
	assert.Equal(t, "ack-2.0.0-current-gate", unacked[0].Key)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), acksObjectKey(), cm))
	assert.Contains(t, cm.Data, "ack-2.0.0-current-gate")
	assert.NotContains(t, cm.Data, "ack-1.0.0-old-gate", "old-version gates must not be written")
}

func TestEnsureGates_RejectsEmptyVersion(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	_, err := gc.EnsureGates(context.Background(), map[string]string{
		"ack-1.0.0-gate": "msg",
	}, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "version must not be empty")
}

func TestEnsureGates_NoMatchingVersion(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), map[string]string{
		"ack-1.0.0-old-gate": "Old gate",
	}, "2.0.0")

	require.NoError(t, err)
	assert.Nil(t, unacked)
}

func TestEnsureGates_LeavesStaleAcks(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: testNamespace},
		Data: map[string]string{
			"ack-1.0.0-old-gate": "true",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), map[string]string{
		"ack-2.0.0-new-gate": "New gate",
	}, "2.0.0")

	require.NoError(t, err)
	require.Len(t, unacked, 1)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), acksObjectKey(), cm))
	assert.Equal(t, "true", cm.Data["ack-1.0.0-old-gate"], "stale acks must be left in place")
	assert.Equal(t, "New gate", cm.Data["ack-2.0.0-new-gate"])
}

func TestIsGateConfigMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cm     *corev1.ConfigMap
		expect bool
	}{
		{
			name:   "nil",
			cm:     nil,
			expect: false,
		},
		{
			name:   "no labels",
			cm:     &corev1.ConfigMap{},
			expect: false,
		},
		{
			name: "wrong label value",
			cm: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					gates.UpgradeGateLabel: "false",
				}},
			},
			expect: false,
		},
		{
			name: "correct label",
			cm: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					gates.UpgradeGateLabel: "true",
				}},
			},
			expect: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expect, gates.IsGateConfigMap(tc.cm))
		})
	}
}

func TestLoadInTreeGates_EmptyData(t *testing.T) {
	t.Parallel()

	result, err := gates.LoadInTreeGates("99.99.99")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestLoadInTreeGates_VersionFilter(t *testing.T) {
	t.Parallel()

	result, err := gates.LoadInTreeGates("0.0.0-nonexistent")
	require.NoError(t, err)
	assert.Empty(t, result, "no gates should match a version that doesn't exist in the embedded file")
}

func TestDiscoverGates_NoLabeledCMs(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	result, err := gc.DiscoverGates(context.Background())
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestDiscoverGates_MultipleLabeledCMs(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gate-source-1",
			Namespace: testNamespace,
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			"ack-2.0.0-api-change": "API changed",
		},
	}
	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gate-source-2",
			Namespace: testNamespace,
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			"ack-2.0.0-storage-migration": "Storage migrated",
		},
	}
	unlabeled := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unrelated-cm",
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"should-not-appear": "ignored",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm1, cm2, unlabeled).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	result, err := gc.DiscoverGates(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "API changed", result["ack-2.0.0-api-change"])
	assert.Equal(t, "Storage migrated", result["ack-2.0.0-storage-migration"])
	assert.NotContains(t, result, "should-not-appear")
}

const testNamespace = "test-ns"

func acksObjectKey() client.ObjectKey {
	return client.ObjectKey{Namespace: testNamespace, Name: gates.AcksConfigMap}
}
