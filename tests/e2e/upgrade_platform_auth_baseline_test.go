package e2e_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

// The baseline store persists digest entries into an in-cluster ConfigMap
// that survives the operator upgrade: the PreUpgrade phase saves it, the
// PostUpgrade phase loads it and compares.
const (
	// upgradeBaselineNamespace hosts the baseline ConfigMap.
	upgradeBaselineNamespace = "upgrade-platform-auth"
	// upgradeBaselineCMName is the baseline ConfigMap's name.
	upgradeBaselineCMName = "upgrade-platform-auth-baseline"
)

const (
	// upgradeBaselineManagedByLabel marks the baseline ConfigMap as owned by
	// this test suite. It is a label only — never an owner reference — so the
	// baseline survives operator uninstall and everything else except
	// explicit deletion.
	upgradeBaselineManagedByLabel = "app.kubernetes.io/managed-by"
	// upgradeBaselineManagedByValue is the managed-by label's value.
	upgradeBaselineManagedByValue = "platform-auth-upgrade-tests"
)

// saveUpgradeBaseline persists entries (digests only — never raw secret
// values) into the baseline ConfigMap, creating the namespace and the
// ConfigMap if absent. Saving always fully replaces the previous Data, so
// pre-phase re-runs are idempotent and cannot leave stale entries behind.
func saveUpgradeBaseline(ctx context.Context, c client.Client, entries map[string]string) error {
	// Create the namespace if absent; AlreadyExists from a concurrent writer
	// is fine.
	if err := c.Get(ctx, types.NamespacedName{Name: upgradeBaselineNamespace}, &corev1.Namespace{}); err != nil {
		if !k8serr.IsNotFound(err) {
			return fmt.Errorf("get baseline namespace %s: %w", upgradeBaselineNamespace, err)
		}
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: upgradeBaselineNamespace},
		}
		if err := c.Create(ctx, namespace); err != nil && !k8serr.IsAlreadyExists(err) {
			return fmt.Errorf("create baseline namespace %s: %w", upgradeBaselineNamespace, err)
		}
	}

	baseline := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Namespace: upgradeBaselineNamespace, Name: upgradeBaselineCMName}, baseline)
	switch {
	case k8serr.IsNotFound(err):
		baseline = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      upgradeBaselineCMName,
				Namespace: upgradeBaselineNamespace,
				Labels:    map[string]string{upgradeBaselineManagedByLabel: upgradeBaselineManagedByValue},
			},
			Data: entries,
		}
		if err := c.Create(ctx, baseline); err != nil {
			return fmt.Errorf("create baseline ConfigMap %s/%s: %w", upgradeBaselineNamespace, upgradeBaselineCMName, err)
		}
	case err != nil:
		return fmt.Errorf("get baseline ConfigMap %s/%s: %w", upgradeBaselineNamespace, upgradeBaselineCMName, err)
	default:
		// Replace Data wholesale so re-runs overwrite the previous baseline.
		baseline.Data = entries
		if baseline.Labels == nil {
			baseline.Labels = map[string]string{}
		}
		baseline.Labels[upgradeBaselineManagedByLabel] = upgradeBaselineManagedByValue
		if err := c.Update(ctx, baseline); err != nil {
			return fmt.Errorf("update baseline ConfigMap %s/%s: %w", upgradeBaselineNamespace, upgradeBaselineCMName, err)
		}
	}

	return nil
}

// loadUpgradeBaseline returns the digest entries persisted by the PreUpgrade
// phase. It fails with a distinct error when the baseline is missing (either
// the namespace or the ConfigMap is absent — a ConfigMap Get in a missing
// namespace is also NotFound), so a PostUpgrade run without its PreUpgrade
// counterpart cannot be mistaken for a transient glitch. The error names only
// resource identifiers, never entry values.
func loadUpgradeBaseline(ctx context.Context, c client.Client) (map[string]string, error) {
	baseline := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Namespace: upgradeBaselineNamespace, Name: upgradeBaselineCMName}, baseline)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil, fmt.Errorf(
				"no baseline found — was the PreUpgrade phase run before the upgrade? (ConfigMap %s/%s): %w",
				upgradeBaselineNamespace, upgradeBaselineCMName, err,
			)
		}
		return nil, fmt.Errorf("get baseline ConfigMap %s/%s: %w", upgradeBaselineNamespace, upgradeBaselineCMName, err)
	}

	return baseline.Data, nil
}

// TestSaveAndLoadUpgradeBaseline proves the store round-trips digest entries
// through the in-cluster ConfigMap: save creates the namespace and the labeled
// ConfigMap, and load returns exactly the saved entries. The ConfigMap must
// carry no owner references — it has to survive operator uninstall and every
// other upgrade-side churn except explicit deletion.
func TestSaveAndLoadUpgradeBaseline(t *testing.T) {
	cli, err := fakeclient.New()
	require.NoError(t, err)

	entries := map[string]string{
		"--cookie-name":          sha256Hex("_oauth2_proxy"),
		"OAUTH2_PROXY_CLIENT_ID": sha256Hex("data-science"),
	}

	require.NoError(t, saveUpgradeBaseline(t.Context(), cli, entries))

	namespaceObject := &corev1.Namespace{}
	require.NoError(t, cli.Get(t.Context(), types.NamespacedName{Name: upgradeBaselineNamespace}, namespaceObject),
		"save must create the baseline namespace when absent")

	baseline := &corev1.ConfigMap{}
	require.NoError(t, cli.Get(t.Context(),
		types.NamespacedName{Namespace: upgradeBaselineNamespace, Name: upgradeBaselineCMName}, baseline))
	require.Equal(t, "platform-auth-upgrade-tests", baseline.Labels["app.kubernetes.io/managed-by"])
	require.Empty(t, baseline.OwnerReferences, "no owner references: the baseline must survive everything except explicit deletion")
	require.Equal(t, entries, baseline.Data, "only digest entries are persisted, with no transformation")

	loaded, err := loadUpgradeBaseline(t.Context(), cli)
	require.NoError(t, err)
	require.Equal(t, entries, loaded)
}

// TestLoadUpgradeBaselineMissing verifies that loading before the PreUpgrade
// phase ran (namespace and/or ConfigMap absent) fails with the distinct
// "no baseline found" error pointing at the missing phase, not with a bare
// NotFound the caller could mistake for a transient glitch.
func TestLoadUpgradeBaselineMissing(t *testing.T) {
	cli, err := fakeclient.New()
	require.NoError(t, err)

	loaded, err := loadUpgradeBaseline(t.Context(), cli)
	require.Error(t, err)
	assert.Nil(t, loaded)
	assert.Contains(t, err.Error(), "no baseline found")
	assert.Contains(t, err.Error(), "PreUpgrade")
}

// TestSaveUpgradeBaselineOverwrites proves pre-phase re-runs are idempotent:
// saving again fully replaces the previous baseline instead of merging into
// it, so entries dropped by a newer run cannot linger as stale evidence.
func TestSaveUpgradeBaselineOverwrites(t *testing.T) {
	cli, err := fakeclient.New()
	require.NoError(t, err)

	first := map[string]string{
		"--cookie-name": sha256Hex("old-cookie-name"),
		"--upstream":    sha256Hex("http://old-upstream"),
	}
	second := map[string]string{
		"--cookie-name": sha256Hex("new-cookie-name"),
	}

	require.NoError(t, saveUpgradeBaseline(t.Context(), cli, first))
	require.NoError(t, saveUpgradeBaseline(t.Context(), cli, second))

	loaded, err := loadUpgradeBaseline(t.Context(), cli)
	require.NoError(t, err)
	require.Equal(t, second, loaded, "a pre-phase re-run must fully overwrite the previous baseline")
}
