package provision

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

// ExtractUpgradeGates scans rr.Resources for ConfigMaps carrying the
// upgrade-gate label, collects their data entries into rr.GateEntries,
// and removes the gate CMs from rr.Resources so they are not deployed
// as standalone objects.
//
// This action is placed after helm/kustomize render and before
// checkUpgradeGates in the modules action chain so that gate CMs
// embedded in module charts are discovered before the gate check runs.
func ExtractUpgradeGates(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	kept := make([]unstructured.Unstructured, 0, len(rr.Resources))

	for i := range rr.Resources {
		res := &rr.Resources[i]
		if res.GetKind() != "ConfigMap" || res.GetLabels()[gates.UpgradeGateLabel] != "true" {
			kept = append(kept, *res)
			continue
		}

		if rr.GateEntries == nil {
			rr.GateEntries = make(map[string]string)
		}

		data, found, err := unstructured.NestedStringMap(res.Object, "data")
		if err != nil {
			log.Error(err, "gate ConfigMap has non-string data entries, skipping",
				"name", res.GetName())
			kept = append(kept, *res)

			continue
		}

		if !found {
			continue
		}

		for k, v := range data {
			rr.GateEntries[k] = v
		}

		log.V(1).Info("extracted gate ConfigMap from rendered resources",
			"name", res.GetName(), "entries", len(data))
	}

	if rr.GateEntries != nil {
		rr.Resources = kept
	}

	return nil
}

// ComponentUpgradeGateAction is an actions.Fn suitable for component
// controller action chains. It blocks reconciliation when the
// odh-upgrade-acks ConfigMap has unacknowledged entries, or when the
// ConfigMap does not yet exist (the modules controller has not yet
// completed its gate evaluation). This is a lightweight read-only
// check — it never creates or modifies the ConfigMap.
func ComponentUpgradeGateAction(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if !flags.IsDSCEnabled() {
		return nil
	}

	ns, err := cluster.GetOperatorNamespace()
	if err != nil {
		return nil //nolint:nilerr // operator NS not initialized (e.g. tests); skip gracefully
	}

	deployed, err := cluster.GetDeployedRelease(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to determine deployed release for upgrade gates: %w", err)
	}
	targetVersion, err := ResolveUpgradeGateVersion(ctx, rr.Client, ns, deployed, rr.Release)
	if err != nil {
		return fmt.Errorf("failed to resolve upgrade gate version: %w", err)
	}

	return ComponentUpgradeGateCheck(ctx, rr.Client, ns, targetVersion, rr.Conditions)
}

// ComponentUpgradeGateCheck is the namespace-explicit variant of
// ComponentUpgradeGateAction, suitable for direct testing.
func ComponentUpgradeGateCheck(
	ctx context.Context,
	cli client.Client,
	namespace string,
	targetVersion string,
	conditions ConditionWriter,
) error {
	cleared, err := gates.AllGatesAcknowledged(ctx, cli, namespace, targetVersion)
	if err != nil {
		return fmt.Errorf("failed to check upgrade gates: %w", err)
	}

	if !cleared {
		conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeProvisioningProgress,
			Status:  metav1.ConditionFalse,
			Reason:  status.AdminAckRequiredReason,
			Message: "Waiting for upgrade gates to be acknowledged",
		})
		conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeProvisioningSucceeded,
			Status:  metav1.ConditionFalse,
			Reason:  status.AdminAckRequiredReason,
			Message: "Waiting for upgrade gates to be acknowledged",
		})

		return odherrors.NewStopError("waiting for upgrade gates to be acknowledged")
	}

	return nil
}

// CheckUpgradeGates evaluates admin-acknowledgment gates for the current
// operator version. It collects gates from all sources (in-tree, labeled
// cluster ConfigMaps, chart-extracted entries), writes their descriptions
// into odh-upgrade-acks (preserving "true" values), and blocks
// provisioning if any gates remain unacknowledged.
func CheckUpgradeGates(ctx context.Context, cli client.Client, release common.Release, conditions ConditionWriter, chartGates map[string]string) error {
	ns, err := cluster.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("cannot check upgrade gates: %w", err)
	}

	return CheckUpgradeGatesInNamespace(ctx, cli, ns, release, conditions, chartGates)
}

// CheckUpgradeGatesInNamespace is the namespace-explicit variant of
// CheckUpgradeGates. Gates apply when upgrading to any newer release; equal
// versions, downgrades, and fresh installs are allowed through without
// blocking.
func CheckUpgradeGatesInNamespace(
	ctx context.Context, cli client.Client, namespace string,
	release common.Release, conditions ConditionWriter,
	chartGates map[string]string,
) error {
	log := logf.FromContext(ctx)

	gc := gates.NewGateChecker(cli, namespace)

	deployed, err := cluster.GetDeployedRelease(ctx, cli)
	if err != nil {
		return fmt.Errorf("cannot determine deployed release for upgrade gate check: %w", err)
	}

	targetVersion, err := ResolveUpgradeGateVersion(ctx, cli, namespace, deployed, release)
	if err != nil {
		return fmt.Errorf("failed to resolve upgrade gate version: %w", err)
	}

	if !isVersionUpgrade(deployed.Version.Version, targetVersion) {
		// Not a version upgrade — create empty ConfigMap to signal
		// "gate evaluation complete, no gates needed" so component
		// controllers waiting on the ConfigMap can proceed.
		if _, err := gc.EnsureGates(ctx, nil); err != nil {
			return fmt.Errorf("failed to create empty upgrade gates ConfigMap: %w", err)
		}
		return nil
	}

	allGates := make(map[string]string)

	// In-tree gates are embedded in the binary and are already scoped by the
	// gate definitions. This ensures they are evaluated even when OLM's
	// two-step CSV rollout causes the running release to temporarily report an
	// old version.
	intreeGates, err := gates.LoadInTreeGates()
	if err != nil {
		return fmt.Errorf("failed to load in-tree gates: %w", err)
	}
	for k, v := range intreeGates {
		allGates[k] = v
	}

	// Cluster-labeled and chart-extracted gates are version-filtered
	// to avoid stale entries from previous upgrades blocking a new one.
	clusterGates, err := gc.DiscoverGates(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover cluster gates: %w", err)
	}
	for k, v := range clusterGates {
		if _, matches := gates.MatchGateKey(k, targetVersion); matches {
			allGates[k] = v
		}
	}

	for k, v := range chartGates {
		if _, matches := gates.MatchGateKey(k, targetVersion); matches {
			allGates[k] = v
		}
	}

	unacked, err := gc.EnsureGates(ctx, allGates)
	if err != nil {
		return fmt.Errorf("failed to ensure upgrade gates: %w", err)
	}

	if len(unacked) == 0 {
		return nil
	}

	keys := make([]string, len(unacked))
	for i, g := range unacked {
		keys[i] = g.Key
	}

	log.Info("provisioning blocked by unacknowledged upgrade gates",
		"version", targetVersion,
		"unacked_gates", keys,
	)

	conditions.SetCondition(common.Condition{
		Type:    status.ConditionTypeProvisioningProgress,
		Status:  metav1.ConditionFalse,
		Reason:  status.AdminAckRequiredReason,
		Message: fmt.Sprintf("Upgrade gates not acknowledged: %s", strings.Join(keys, ", ")),
	})

	return odherrors.NewStopError("provisioning blocked: %d unacknowledged upgrade gate(s) for version %s", len(unacked), targetVersion)
}
