package provision

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// AutoAcknowledgeUpgradeGates is an actions.Fn that auto-acknowledges
// upgrade gates for components whose health checks pass. Components
// that are not Managed in the DSC are auto-acked immediately since
// they are not expected to be present. It is a no-op when the acks
// ConfigMap does not exist or has no unacknowledged entries for the
// current version (fresh install or same-major upgrade).
func AutoAcknowledgeUpgradeGates(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	ns, nsErr := cluster.GetOperatorNamespace()
	if nsErr != nil {
		return nil //nolint:nilerr // operator NS not initialized (e.g. tests); skip gracefully
	}

	// Auto-ack must read the latest gate state from the API server before deciding whether to update it.
	reader := rr.Controller.GetAPIReader()

	cm := &corev1.ConfigMap{}
	err := reader.Get(ctx, client.ObjectKey{Name: gates.AcksConfigMap, Namespace: ns}, cm)
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("failed to get %s ConfigMap: %w", gates.AcksConfigMap, err)
	}

	original := cm.DeepCopy()
	if err := AutoAcknowledgeUpgradeGatesInNamespace(
		ctx,
		reader,
		cm,
		cluster.GetApplicationNamespace(),
		UpgradeGateVersion,
		resolveManagedComponents(rr.Instance),
	); err != nil {
		return err
	}

	if reflect.DeepEqual(original.Data, cm.Data) {
		return nil
	}

	if err := rr.Client.Update(ctx, cm); err != nil {
		return fmt.Errorf("failed to patch %s ConfigMap: %w", gates.AcksConfigMap, err)
	}

	return nil
}

// componentStates maps known DSC component names to their management state.
// Gate keys that resolve to Removed components are auto-acknowledged.
// Gate keys that do not match a DSC component remain in scope and still run
// through the registered gate check path.
func AutoAcknowledgeUpgradeGatesInNamespace(
	ctx context.Context,
	reader client.Reader,
	cm *corev1.ConfigMap,
	appsNS string,
	version string,
	componentStates map[string]operatorv1.ManagementState,
) error {
	if cm == nil {
		return nil
	}

	log := logf.FromContext(ctx)

	versionPrefix := "ack-" + version + "-"
	log.Info("running auto-ack upgrade checks", "version", version)

	for key, value := range cm.Data {
		if !strings.HasPrefix(key, versionPrefix) || value == "true" {
			continue
		}

		gateKey := strings.TrimPrefix(key, versionPrefix)
		if gateKey == "" {
			continue
		}

		if state, known := componentStates[gateKey]; known && state == operatorv1.Removed {
			cm.Data[key] = "true"

			log.Info("auto-acknowledged upgrade gate for removed component",
				"key", key, "component", gateKey)

			continue
		}

		checkFn := GetUpgradeCheck(gateKey)

		if err := checkFn(ctx, reader, gateKey, appsNS); err != nil {
			log.V(1).Info("gate not ready for auto-ack",
				"key", key,
				"component", gateKey,
				"reason", err,
			)

			cm.Data[key] = err.Error()

			continue
		}

		cm.Data[key] = "true"

		log.Info("auto-acknowledged upgrade gate", "key", key, "component", gateKey)
	}

	return nil
}

// resolveManagedComponents extracts DSC component management states keyed by
// component name. Returns nil when the instance is not a DSC (e.g. Platform CR
// in xKS mode), which means all gate keys remain in scope for registered checks.
func resolveManagedComponents(instance client.Object) map[string]operatorv1.ManagementState {
	dsc, ok := instance.(*dscv2.DataScienceCluster)
	if !ok || dsc == nil {
		return nil
	}

	result := make(map[string]operatorv1.ManagementState)

	componentsValue := reflect.ValueOf(dsc.Spec.Components)
	componentsType := componentsValue.Type()

	for i := range componentsType.NumField() {
		field := componentsType.Field(i)

		jsonTag := field.Tag.Get("json")
		name := strings.Split(jsonTag, ",")[0]
		if name == "" || name == "-" {
			continue
		}

		mgmtField := componentsValue.Field(i).FieldByName("ManagementState")
		if !mgmtField.IsValid() {
			continue
		}

		state := operatorv1.Removed
		if value, ok := mgmtField.Interface().(operatorv1.ManagementState); ok && value != "" {
			state = value
		}
		result[name] = state
	}

	return result
}
