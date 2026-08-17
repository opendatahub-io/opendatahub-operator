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

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
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
	ns, err := cluster.GetOperatorNamespace()
	if err != nil {
		return nil //nolint:nilerr // operator NS not initialized (e.g. tests); skip gracefully
	}

	appsNS := cluster.GetApplicationNamespace()
	managed := resolveManagedComponents(rr.Instance)
	reader := client.Reader(rr.Client)
	if rr.Controller != nil && rr.Controller.GetAPIReader() != nil {
		reader = rr.Controller.GetAPIReader()
	}

	return AutoAcknowledgeUpgradeGatesInNamespace(
		ctx, rr.Client, reader, ns, appsNS, rr.Release.Version.String(), managed)
}

// managedComponents maps component names to true
// when they are Managed in the DSC. A nil map means
// all components require a health check.
func AutoAcknowledgeUpgradeGatesInNamespace(
	ctx context.Context, cli client.Client,
	reader client.Reader,
	operatorNS, appsNS, version string,
	managedComponents map[string]bool,
) error {
	log := logf.FromContext(ctx)
	if reader == nil {
		reader = cli
	}

	acksCM := &corev1.ConfigMap{}
	if err := cli.Get(ctx, client.ObjectKey{
		Name:      gates.AcksConfigMap,
		Namespace: operatorNS,
	}, acksCM); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get %s ConfigMap: %w", gates.AcksConfigMap, err)
	}

	versionPrefix := "ack-" + version + "-"
	unacked := collectUnackedComponents(acksCM.Data, versionPrefix)

	if len(unacked) == 0 {
		return nil
	}

	log.Info("running auto-ack upgrade checks", "version", version, "unacked", len(unacked))

	dirty := false
	for key, component := range unacked {
		if !requiresCheckWhenUnmanaged(component) && (managedComponents == nil || !managedComponents[component]) {
			acksCM.Data[key] = "true"
			dirty = true

			log.Info("auto-acknowledged upgrade gate for unmanaged component",
				"key", key, "component", component)

			continue
		}

		checkFn := GetUpgradeCheck(component)

		if err := checkFn(ctx, reader, component, appsNS); err != nil {
			log.V(1).Info("component not ready for auto-ack",
				"component", component, "reason", err)
			continue
		}

		acksCM.Data[key] = "true"
		dirty = true

		log.Info("auto-acknowledged upgrade gate", "key", key, "component", component)
	}

	if dirty {
		if err := cli.Update(ctx, acksCM); err != nil {
			return fmt.Errorf("failed to patch %s ConfigMap: %w", gates.AcksConfigMap, err)
		}
	}

	return nil
}

func requiresCheckWhenUnmanaged(component string) bool {
	switch component {
	case componentApi.ModelMeshServingComponentName:
		return true
	case strings.ToLower(componentApi.CodeFlareKind):
		return true
	default:
		return false
	}
}

// collectUnackedComponents returns a map of gate-key → component-name
// for entries that match the version prefix and are not yet acknowledged.
func collectUnackedComponents(data map[string]string, versionPrefix string) map[string]string {
	result := make(map[string]string)
	for key, val := range data {
		if !strings.HasPrefix(key, versionPrefix) {
			continue
		}
		if val == "true" {
			continue
		}
		component := strings.TrimPrefix(key, versionPrefix)
		if component != "" {
			result[key] = component
		}
	}
	return result
}

// resolveManagedComponents extracts a set of component names that have
// ManagementState == Managed from the DSC spec. Returns nil when the
// instance is not a DSC (e.g. Platform CR in xKS mode), which means
// all components will require a health check.
func resolveManagedComponents(instance client.Object) map[string]bool {
	dsc, ok := instance.(*dscv2.DataScienceCluster)
	if !ok || dsc == nil {
		return nil
	}

	result := make(map[string]bool)

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

		if mgmtField.Interface() == operatorv1.Managed {
			result[name] = true
		}
	}

	return result
}
