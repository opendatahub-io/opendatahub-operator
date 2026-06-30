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
// CheckUpgradeGates.
func CheckUpgradeGatesInNamespace(
	ctx context.Context, cli client.Client, namespace string,
	release common.Release, conditions ConditionWriter,
	chartGates map[string]string,
) error {
	log := logf.FromContext(ctx)

	gc := gates.NewGateChecker(cli, namespace)
	version := release.Version.String()

	allGates := make(map[string]string)

	intreeGates, err := gates.LoadInTreeGates(version)
	if err != nil {
		return fmt.Errorf("failed to load in-tree gates: %w", err)
	}
	for k, v := range intreeGates {
		allGates[k] = v
	}

	clusterGates, err := gc.DiscoverGates(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover cluster gates: %w", err)
	}
	for k, v := range clusterGates {
		allGates[k] = v
	}

	for k, v := range chartGates {
		allGates[k] = v
	}

	unacked, err := gc.EnsureGates(ctx, allGates, version)
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
		"version", version,
		"unacked_gates", keys,
	)

	conditions.SetCondition(common.Condition{
		Type:    status.ConditionTypeProvisioningProgress,
		Status:  metav1.ConditionFalse,
		Reason:  status.AdminAckRequiredReason,
		Message: fmt.Sprintf("Upgrade gates not acknowledged: %s", strings.Join(keys, ", ")),
	})

	return odherrors.NewStopError("provisioning blocked: %d unacknowledged upgrade gate(s) for version %s", len(unacked), version)
}
