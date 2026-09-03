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
	})

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
	})

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
	})

	require.NoError(t, err)
	assert.Empty(t, unacked)
}

func TestEnsureGates_EmptyEntries_CreatesConfigMap(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), map[string]string{})

	require.NoError(t, err)
	assert.Empty(t, unacked)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), acksObjectKey(), cm),
		"empty EnsureGates must still create the ConfigMap")
}

func TestEnsureGates_NilEntries_CreatesConfigMap(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), nil)

	require.NoError(t, err)
	assert.Empty(t, unacked)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), acksObjectKey(), cm),
		"nil EnsureGates must still create the ConfigMap")
}

func TestEnsureGates_WritesAllEntriesWithoutFiltering(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), map[string]string{
		"ack-1.0.0-old-gate":     "Old gate",
		"ack-2.0.0-current-gate": "Current gate",
	})

	require.NoError(t, err)
	require.Len(t, unacked, 2)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), acksObjectKey(), cm))
	assert.Contains(t, cm.Data, "ack-2.0.0-current-gate")
	assert.Contains(t, cm.Data, "ack-1.0.0-old-gate", "EnsureGates no longer filters by version")
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
	})

	require.NoError(t, err)
	require.Len(t, unacked, 1)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), acksObjectKey(), cm))
	assert.Equal(t, "true", cm.Data["ack-1.0.0-old-gate"], "stale acks must be left in place")
	assert.Equal(t, "New gate", cm.Data["ack-2.0.0-new-gate"])
}

func TestEnsureGates_PreservesErrorMessageValues(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: testNamespace},
		Data: map[string]string{
			"ack-2.0.0-ray":      "1 CodeFlare-managed RayClusters still require pre-upgrade backup",
			"ack-2.0.0-trustyai": "2 TrustyAIService instances using PVC storage require backup",
			"ack-2.0.0-kserve":   "true",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), map[string]string{
		"ack-2.0.0-ray":      "Acknowledge upgrade of ray",
		"ack-2.0.0-trustyai": "Acknowledge upgrade of trustyai",
		"ack-2.0.0-kserve":   "Acknowledge upgrade of kserve",
	})

	require.NoError(t, err)
	require.Len(t, unacked, 2)

	cm := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(context.Background(), acksObjectKey(), cm))
	assert.Equal(t, "1 CodeFlare-managed RayClusters still require pre-upgrade backup", cm.Data["ack-2.0.0-ray"],
		"error message must not be overwritten by gate description")
	assert.Equal(t, "2 TrustyAIService instances using PVC storage require backup", cm.Data["ack-2.0.0-trustyai"],
		"error message must not be overwritten by gate description")
	assert.Equal(t, "true", cm.Data["ack-2.0.0-kserve"],
		"acked entry must remain acknowledged")
}

func TestMatchGateKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		key      string
		version  string
		gateName string
		match    bool
	}{
		{name: "minor first patch", key: "ack-3.5-kserve", version: "3.5.1", gateName: "kserve", match: true},
		{name: "minor later patch", key: "ack-3.5-kserve", version: "3.5.2", gateName: "kserve", match: true},
		{name: "different minor", key: "ack-3.5-kserve", version: "3.6.0", match: false},
		{name: "exact match", key: "ack-3.5.1-kserve", version: "3.5.1", gateName: "kserve", match: true},
		{name: "different patch", key: "ack-3.5.1-kserve", version: "3.5.2", match: false},
		{name: "multi-digit patch", key: "ack-3.5.10-kserve", version: "3.5.10", gateName: "kserve", match: true},
		{name: "multi-digit patch does not prefix-match", key: "ack-3.5.10-kserve", version: "3.5.1", match: false},
		{name: "prerelease uses numeric core", key: "ack-3.5.2-kserve", version: "3.5.2-rc.1", gateName: "kserve", match: true},
		{name: "build metadata uses numeric core", key: "ack-3.5-kserve", version: "3.5.2+build.7", gateName: "kserve", match: true},
		{name: "missing prefix", key: "3.5-kserve", version: "3.5.1", match: false},
		{name: "major only", key: "ack-3-kserve", version: "3.5.1", match: false},
		{name: "empty patch", key: "ack-3.5.-kserve", version: "3.5.1", match: false},
		{name: "version prefix", key: "ack-v3.5-kserve", version: "3.5.1", match: false},
		{name: "missing gate name", key: "ack-3.5-", version: "3.5.1", match: false},
		{name: "invalid current version", key: "ack-3.5-kserve", version: "3.5", match: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gateName, match := gates.MatchGateKey(tc.key, tc.version)
			assert.Equal(t, tc.match, match)
			assert.Equal(t, tc.gateName, gateName)
		})
	}
}

func TestLoadInTreeGates_ReturnsAllEntries(t *testing.T) {
	t.Parallel()

	result, err := gates.LoadInTreeGates()
	require.NoError(t, err)
	assert.NotEmpty(t, result, "embedded gates.yaml should contain entries")
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

func TestEnsureGatesNil_ThenAllGatesAcknowledged_ReturnsTrue(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	gc := gates.NewGateChecker(cli, testNamespace)

	unacked, err := gc.EnsureGates(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, unacked)

	cleared, err := gates.AllGatesAcknowledged(context.Background(), cli, testNamespace, "3.5.1")
	require.NoError(t, err)
	assert.True(t, cleared, "AllGatesAcknowledged must return true after EnsureGates(nil) creates an empty ConfigMap")
}

func TestAllGatesAcknowledged_NoCMReturnsFalse(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()

	cleared, err := gates.AllGatesAcknowledged(context.Background(), cli, testNamespace, "3.5.1")
	require.NoError(t, err)
	assert.False(t, cleared, "missing acks CM means modules controller hasn't evaluated yet")
}

func TestAllGatesAcknowledged_EmptyCMReturnsTrue(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: testNamespace},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

	cleared, err := gates.AllGatesAcknowledged(context.Background(), cli, testNamespace, "3.5.1")
	require.NoError(t, err)
	assert.True(t, cleared, "empty CM means no gates needed (fresh install)")
}

func TestAllGatesAcknowledged_AllAcked(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: testNamespace},
		Data: map[string]string{
			"ack-3.5.1-kserve": "true",
			"ack-3.5.1-trusty": "true",
			"ack-3.5.1-module": "true",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

	cleared, err := gates.AllGatesAcknowledged(context.Background(), cli, testNamespace, "3.5.1")
	require.NoError(t, err)
	assert.True(t, cleared)
}

func TestAllGatesAcknowledged_PartiallyAcked(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: testNamespace},
		Data: map[string]string{
			"ack-3.5.1-kserve": "true",
			"ack-3.5.1-trusty": "Acknowledge upgrade of trustyai",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

	cleared, err := gates.AllGatesAcknowledged(context.Background(), cli, testNamespace, "3.5.1")
	require.NoError(t, err)
	assert.False(t, cleared)
}

func TestAllGatesAcknowledged_IgnoresNonTargetVersions(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: gates.AcksConfigMap, Namespace: testNamespace},
		Data: map[string]string{
			"ack-3.5.1-patch-only": "not acknowledged",
			"ack-3.5-shared":       "true",
			"ack-3.6-future":       "not acknowledged",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

	cleared, err := gates.AllGatesAcknowledged(context.Background(), cli, testNamespace, "3.5.2")
	require.NoError(t, err)
	assert.True(t, cleared)
}

const testNamespace = "test-ns"

func acksObjectKey() client.ObjectKey {
	return client.ObjectKey{Namespace: testNamespace, Name: gates.AcksConfigMap}
}
