package upgrade

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhgvk "github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	// DeleteConfigMapLabel is the label for configMap used to trigger operator uninstall
	// TODO: Label should be updated if addon name changes.
	DeleteConfigMapLabel = "api.openshift.com/addon-managed-odh-delete"
)

// OperatorUninstall deletes all the externally generated resources.
// This includes DSCI, namespace created by operator (but not workbench or MR's), subscription and CSV.
func OperatorUninstall(ctx context.Context, cli client.Client, platform common.Platform) error {
	log := logf.FromContext(ctx)

	if err := removeDSC(ctx, cli); err != nil {
		return err
	}

	if err := removeDSCI(ctx, cli); err != nil {
		return err
	}

	// Delete generated namespaces by the operator
	generatedNamespaces := &corev1.NamespaceList{}
	nsOptions := []client.ListOption{
		client.MatchingLabels{labels.ODH.OwnedNamespace: "true"},
	}
	if err := cli.List(ctx, generatedNamespaces, nsOptions...); err != nil {
		return fmt.Errorf("error getting generated namespaces : %w", err)
	}

	// Return if any one of the namespaces is Terminating due to resources that are in process of deletion. (e.g. CRDs)
	for _, namespace := range generatedNamespaces.Items {
		if namespace.Status.Phase == corev1.NamespaceTerminating {
			return fmt.Errorf("waiting for namespace %v to be deleted", namespace.Name)
		}
	}

	for _, namespace := range generatedNamespaces.Items {
		if namespace.Status.Phase == corev1.NamespaceActive {
			if err := cli.Delete(ctx, &namespace); err != nil {
				return fmt.Errorf("error deleting namespace %v: %w", namespace.Name, err)
			}
			log.Info("Namespace deleted as a part of uninstallation", "namespace", namespace.Name)
		}
	}

	// give enough time for namespace deletion before proceed
	time.Sleep(10 * time.Second)

	// We can only assume the subscription is using standard names
	// if user install by creating different named subs, then we will not know the name
	// we cannot remove CSV before remove subscription because that need SA account
	operatorNs, err := cluster.GetOperatorNamespace()
	if err != nil {
		return err
	}

	log.Info("Removing operator subscription which in turn will remove installplan")
	subsName := "opendatahub-operator"
	if platform == cluster.SelfManagedRhoai {
		subsName = "rhods-operator"
	}
	if platform != cluster.ManagedRhoai {
		if err := cluster.DeleteExistingSubscription(ctx, cli, operatorNs, subsName); err != nil {
			return err
		}
	}

	log.Info("Removing the operator CSV in turn remove operator deployment")
	err = removeCSV(ctx, cli)

	log.Info("All resources deleted as part of uninstall.")
	return err
}

func removeDSCI(ctx context.Context, cli client.Client) error {
	instance := &dsciv2.DSCInitialization{}

	if err := cli.DeleteAllOf(ctx, instance, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		return fmt.Errorf("failure deleting DSCI: %w", err)
	}

	return nil
}

type moduleCR struct {
	gvk  schema.GroupVersionKind
	name string
}

// knownModuleCRs lists all module CRs whose lifecycle is managed by the
// modules controller. Their finalizers are set by external module operators
// (separate Deployments) that may be garbage-collected before they can
// process the finalizers during uninstall.
var knownModuleCRs = []moduleCR{
	{gvk: odhgvk.Dashboard, name: componentApi.DashboardInstanceName},
	{gvk: odhgvk.Workbenches, name: componentApi.WorkbenchesInstanceName},
	{gvk: odhgvk.Kserve, name: componentApi.KserveInstanceName},
	{gvk: odhgvk.MLflowOperator, name: componentApi.MLflowOperatorInstanceName},
	{gvk: odhgvk.FeastOperator, name: componentApi.FeastOperatorInstanceName},
	{gvk: odhgvk.OGX, name: componentApi.OGXInstanceName},
	{gvk: odhgvk.AIGateway, name: componentApi.AIGatewayInstanceName},
	{gvk: odhgvk.MCPLifecycleOperator, name: componentApi.MCPLifecycleOperatorInstanceName},
	{gvk: odhgvk.Trainer, name: componentApi.TrainerInstanceName},
}

// removeModuleCRFinalizers strips finalizers from all known module CRs.
// During uninstall with foregroundDeletion, module operator Deployments
// (which are also owned by the DSC) may be garbage-collected before they
// can process the finalizers on their module CRs. This causes the DSC
// deletion to hang indefinitely. Removing the finalizers pre-emptively
// allows the garbage collector to delete the module CRs cleanly.
func removeModuleCRFinalizers(ctx context.Context, cli client.Client) {
	log := logf.FromContext(ctx)

	for _, cr := range knownModuleCRs {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(cr.gvk)

		if err := cli.Get(ctx, client.ObjectKey{Name: cr.name}, obj); err != nil {
			if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
				continue
			}
			log.V(1).Info("Skipping finalizer removal, unable to get module CR",
				"kind", cr.gvk.Kind, "name", cr.name, "error", err)
			continue
		}

		if len(obj.GetFinalizers()) == 0 {
			continue
		}

		log.Info("Removing finalizers from module CR to unblock uninstall",
			"kind", cr.gvk.Kind, "name", cr.name, "finalizers", obj.GetFinalizers())

		patch := client.MergeFrom(obj.DeepCopy())
		obj.SetFinalizers(nil)
		if err := cli.Patch(ctx, obj, patch); err != nil {
			if k8serr.IsNotFound(err) {
				continue
			}
			log.Error(err, "Failed to remove finalizers from module CR",
				"kind", cr.gvk.Kind, "name", cr.name)
		}
	}
}

func removeDSC(ctx context.Context, cli client.Client) error {
	log := logf.FromContext(ctx)
	instance := &dscv2.DataScienceCluster{}

	removeModuleCRFinalizers(ctx, cli)

	if err := cli.DeleteAllOf(ctx, instance, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		return fmt.Errorf("failure deleting DSC: %w", err)
	}

	// The DSCI validating webhook denies DSCI deletion while any DSC still exists.
	// Wait for all DSC objects to be fully removed before returning.
	backoff := wait.Backoff{
		Duration: 2 * time.Second,
		Factor:   2.0,
		Steps:    7,
	}

	var remainingNames []string

	if err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		dscList := &dscv2.DataScienceClusterList{}
		if err := cli.List(ctx, dscList); err != nil {
			return false, err
		}
		if len(dscList.Items) == 0 {
			return true, nil
		}
		remainingNames = make([]string, len(dscList.Items))
		for i := range dscList.Items {
			remainingNames[i] = dscList.Items[i].Name
		}
		log.Info("Waiting for DataScienceCluster objects to be deleted", "remaining", remainingNames)
		return false, nil
	}); err != nil {
		return fmt.Errorf("failure waiting for DSC deletion, remaining objects %v: %w", remainingNames, err)
	}

	return nil
}

// HasDeleteConfigMap returns true if delete configMap is added to the operator namespace by managed-tenants repo.
// It returns false in all other cases.
func HasDeleteConfigMap(ctx context.Context, c client.Client) bool {
	// Get watchNamespace
	operatorNamespace, err := cluster.GetOperatorNamespace()
	if err != nil {
		return false
	}

	// If delete configMap is added, uninstall the operator and the resources
	deleteConfigMapList := &corev1.ConfigMapList{}
	cmOptions := []client.ListOption{
		client.InNamespace(operatorNamespace),
		client.MatchingLabels{DeleteConfigMapLabel: "true"},
	}

	if err := c.List(ctx, deleteConfigMapList, cmOptions...); err != nil {
		return false
	}

	return len(deleteConfigMapList.Items) != 0
}

func removeCSV(ctx context.Context, c client.Client) error {
	log := logf.FromContext(ctx)
	// Get watchNamespace
	operatorNamespace, err := cluster.GetOperatorNamespace()
	if err != nil {
		return err
	}

	operatorCsv, err := cluster.GetClusterServiceVersion(ctx, c, operatorNamespace)
	if k8serr.IsNotFound(err) {
		ctrl.Log.Info("No clusterserviceversion for the operator found.")
		return nil
	}

	if err != nil {
		return err
	}

	log.Info("Deleting CSV", "name", operatorCsv.Name)
	err = c.Delete(ctx, operatorCsv)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("error deleting clusterserviceversion: %w", err)
	}
	log.Info("Clusterserviceversion deleted as a part of uninstall", "name", operatorCsv.Name)

	return nil
}
