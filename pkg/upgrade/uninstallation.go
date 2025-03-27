package upgrade

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
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
			log.Info("Namespace " + namespace.Name + " deleted as a part of uninstallation.")
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
	instance := &dsciv1.DSCInitialization{}

	if err := cli.DeleteAllOf(ctx, instance, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		return fmt.Errorf("failure deleting DSCI: %w", err)
	}

	return nil
}

func removeDSC(ctx context.Context, cli client.Client) error {
	instance := &dscv1.DataScienceCluster{}

	if err := cli.DeleteAllOf(ctx, instance, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		return fmt.Errorf("failure deleting DSC: %w", err)
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

	log.Info("Deleting CSV " + operatorCsv.Name)
	err = c.Delete(ctx, operatorCsv)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("error deleting clusterserviceversion: %w", err)
	}
	log.Info("Clusterserviceversion " + operatorCsv.Name + " deleted as a part of uninstall")

	return nil
}
