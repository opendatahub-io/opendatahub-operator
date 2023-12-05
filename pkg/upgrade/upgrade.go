package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	// "reflect"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfdefv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	// DeleteConfigMapLabel is the label for configMap used to trigger operator uninstall
	// TODO: Label should be updated if addon name changes
	DeleteConfigMapLabel = "api.openshift.com/addon-managed-odh-delete"
	// odhGeneratedNamespaceLabel is the label added to all the namespaces genereated by odh-deployer.
)

// OperatorUninstall deletes all the externally generated resources. This includes monitoring resources and applications
// installed by KfDef.
func OperatorUninstall(cli client.Client, cfg *rest.Config) error {
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}
	// Delete kfdefs if found
	err = RemoveKfDefInstances(cli, platform)
	if err != nil {
		return err
	}

	// Delete DSCInitialization instance
	err = removeDSCInitialization(cli)
	if err != nil {
		return err
	}
	// Delete generated namespaces by the operator
	generatedNamespaces := &corev1.NamespaceList{}
	nsOptions := []client.ListOption{
		client.MatchingLabels{cluster.ODHGeneratedNamespaceLabel: "true"},
	}
	if err := cli.List(context.TODO(), generatedNamespaces, nsOptions...); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("error getting generated namespaces : %v", err)
		}
	}

	// Return if any one of the namespaces is Terminating due to resources that are in process of deletion. (e.g CRDs)
	if len(generatedNamespaces.Items) != 0 {
		for _, namespace := range generatedNamespaces.Items {
			if namespace.Status.Phase == corev1.NamespaceTerminating {
				return fmt.Errorf("waiting for namespace %v to be deleted", namespace.Name)
			}
		}
	}

	// Delete all the active namespaces
	for _, namespace := range generatedNamespaces.Items {
		if namespace.Status.Phase == corev1.NamespaceActive {
			if err := cli.Delete(context.TODO(), &namespace, []client.DeleteOption{}...); err != nil {
				return fmt.Errorf("error deleting namespace %v: %v", namespace.Name, err)
			}
			fmt.Printf("Namespace %s deleted as a part of uninstall.", namespace.Name)
		}
	}

	// Wait for all resources to get cleaned up
	time.Sleep(10 * time.Second)
	fmt.Printf("All resources deleted as part of uninstall. Removing the operator csv")
	return removeCsv(cli, cfg)
}

func removeDSCInitialization(cli client.Client) error {
	// Last check if multiple instances of DSCInitialization exist
	instanceList := &dsci.DSCInitializationList{}
	var err error
	err = cli.List(context.TODO(), instanceList)
	if err != nil {
		return err
	}

	if len(instanceList.Items) != 0 {
		for _, dsciInstance := range instanceList.Items {
			err = cli.Delete(context.TODO(), &dsciInstance)
			if apierrs.IsNotFound(err) {
				err = nil
			}
		}
	}
	return err
}

// HasDeleteConfigMap returns true if delete configMap is added to the operator namespace by managed-tenants repo.
// It returns false in all other cases.
func HasDeleteConfigMap(c client.Client) bool {
	// Get watchNamespace
	operatorNamespace, err := GetOperatorNamespace()
	if err != nil {
		return false
	}

	// If delete configMap is added, uninstall the operator and the resources
	deleteConfigMapList := &corev1.ConfigMapList{}
	cmOptions := []client.ListOption{
		client.InNamespace(operatorNamespace),
		client.MatchingLabels{DeleteConfigMapLabel: "true"},
	}

	if err := c.List(context.TODO(), deleteConfigMapList, cmOptions...); err != nil {
		return false
	}
	return len(deleteConfigMapList.Items) != 0
}

// createDefaultDSC creates a default instance of DSC.
// Note: When the platform is not Managed, and a DSC instance already exists, the function doesn't re-create/update the resource.
func CreateDefaultDSC(cli client.Client, platform deploy.Platform) error {
	// Set the default DSC name depending on the platform
	releaseDataScienceCluster := &dsc.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: "datasciencecluster.opendatahub.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsc",
		},
		Spec: dsc.DataScienceClusterSpec{
			Components: dsc.Components{
				Dashboard: dashboard.Dashboard{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				Workbenches: workbenches.Workbenches{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				ModelMeshServing: modelmeshserving.ModelMeshServing{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
				DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				Kserve: kserve.Kserve{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				CodeFlare: codeflare.CodeFlare{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
				Ray: ray.Ray{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
				TrustyAI: trustyai.TrustyAI{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
			},
		},
	}
	err := cli.Create(context.TODO(), releaseDataScienceCluster)
	switch {
	case err == nil:
		fmt.Printf("created DataScienceCluster resource")
	case apierrs.IsAlreadyExists(err):
		// Do not update the DSC if it already exists
		fmt.Printf("DataScienceCluster resource already exists. It will not be updated with default DSC.")
		return nil
	default:
		return fmt.Errorf("failed to create DataScienceCluster custom resource: %v", err)
	}
	return nil
}

// createDefaultDSCI creates a default instance of DSCI
// If there exists an instance already, it patches the DSCISpec with default values
// Note: DSCI CR modifcations are not supported, as it is the initial prereq setting for the components
func CreateDefaultDSCI(cli client.Client, platform deploy.Platform, appNamespace, monNamespace string) error {
	defaultDsciSpec := &dsci.DSCInitializationSpec{
		ApplicationsNamespace: appNamespace,
		Monitoring: dsci.Monitoring{
			ManagementState: operatorv1.Managed,
			Namespace:       monNamespace,
		},
	}

	defaultDsci := &dsci.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "dscinitialization.opendatahub.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: *defaultDsciSpec,
	}

	patchedDSCI := &dsci.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "dscinitialization.opendatahub.io/v1",
		},
		Spec: *defaultDsciSpec,
	}

	instances := &dsci.DSCInitializationList{}
	if err := cli.List(context.TODO(), instances); err != nil {
		return err
	}

	switch {
	case len(instances.Items) > 1:
		fmt.Printf("only one instance of DSCInitialization object is allowed. Please delete other instances ")
		return nil
	case len(instances.Items) == 1:
		if platform == deploy.ManagedRhods || platform == deploy.SelfManagedRhods {
			data, err := json.Marshal(patchedDSCI)
			if err != nil {
				return err
			}
			existingDSCI := &instances.Items[0]
			err = cli.Patch(context.TODO(), existingDSCI, client.RawPatch(types.ApplyPatchType, data),
				client.ForceOwnership, client.FieldOwner("rhods-operator"))
			if err != nil {
				return err
			}
		} else {
			return nil
		}
	case len(instances.Items) == 0:
		err := cli.Create(context.TODO(), defaultDsci)
		if err != nil {
			return err
		}
	}
	return nil
}

func UpdateFromLegacyVersion(cli client.Client, platform deploy.Platform, appNS string, montNamespace string) error {
	// If platform is Managed, remove Kfdefs and create default dsc
	if platform == deploy.ManagedRhods {
		fmt.Println("starting deletion of Deployment in managed cluster")
		if err := deleteResource(cli, appNS, "deployment"); err != nil {
			return err
		}
		// this is for the modelmesh monitoring part from v1 to v2
		if err := deleteResource(cli, montNamespace, "deployment"); err != nil {
			return err
		}
		if err := deleteResource(cli, montNamespace, "statefulset"); err != nil {
			return err
		}
		fmt.Println("creating default DSC CR")
		if err := CreateDefaultDSC(cli, platform); err != nil {
			return err
		}

		if err := RemoveKfDefInstances(cli, platform); err != nil {
			return err
		}

		return nil
	}

	if platform == deploy.SelfManagedRhods {
		fmt.Println("starting deletion of Deployment in selfmanaged cluster")
		// If KfDef CRD is not found, we see it as a cluster not pre-installed v1 operator	// Check if kfdef are deployed
		kfdefCrd := &apiextv1.CustomResourceDefinition{}
		if err := cli.Get(context.TODO(), client.ObjectKey{Name: "kfdefs.kfdef.apps.kubeflow.org"}, kfdefCrd); err != nil {
			if apierrs.IsNotFound(err) {
				// If no Crd found, return, since its a new Installation
				// return empty list
				return nil
			} else {
				return fmt.Errorf("error retrieving kfdef CRD : %v", err)
			}
		}

		// If KfDef Instances found, and no DSC instances are found in Self-managed, that means this is an upgrade path from
		// legacy version. Create a default DSC instance
		kfDefList := &kfdefv1.KfDefList{}
		err := cli.List(context.TODO(), kfDefList)
		if err != nil {
			if apierrs.IsNotFound(err) {
				// If no KfDefs, do nothing and return
				return nil
			} else {
				return fmt.Errorf("error getting kfdef instances: : %w", err)
			}
		}
		if len(kfDefList.Items) > 0 {
			if err = deleteResource(cli, appNS, "deployment"); err != nil {
				return fmt.Errorf("error deleting deployment: %w", err)
			}
			// this is for the modelmesh monitoring part from v1 to v2
			if err := deleteResource(cli, montNamespace, "deployment"); err != nil {
				return err
			}
			if err := deleteResource(cli, montNamespace, "statefulset"); err != nil {
				return err
			}
			// create default DSC
			if err = CreateDefaultDSC(cli, platform); err != nil {
				return err
			}
		}
		return err
	}
	return nil
}

func GetOperatorNamespace() (string, error) {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns, nil
		}
	}
	return "", err
}

func RemoveKfDefInstances(cli client.Client, platform deploy.Platform) error {
	// Check if kfdef are deployed
	kfdefCrd := &apiextv1.CustomResourceDefinition{}

	err := cli.Get(context.TODO(), client.ObjectKey{Name: "kfdefs.kfdef.apps.kubeflow.org"}, kfdefCrd)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// If no Crd found, return, since its a new Installation
			return nil
		} else {
			return fmt.Errorf("error retrieving kfdef CRD : %v", err)
		}
	} else {
		expectedKfDefList := &kfdefv1.KfDefList{}
		err := cli.List(context.TODO(), expectedKfDefList)
		if err != nil {
			if apierrs.IsNotFound(err) {
				// If no KfDefs, do nothing and return
				return nil
			} else {
				return fmt.Errorf("error getting list of kfdefs: %v", err)
			}
		}
		// Delete kfdefs
		for _, kfdef := range expectedKfDefList.Items {
			// Remove finalizer
			updatedKfDef := &kfdef
			updatedKfDef.Finalizers = []string{}
			err = cli.Update(context.TODO(), updatedKfDef)
			if err != nil {
				return fmt.Errorf("error removing finalizers from kfdef %v : %v", kfdef.Name, err)
			}
			err = cli.Delete(context.TODO(), updatedKfDef)
			if err != nil {
				return fmt.Errorf("error deleting kfdef %v : %v", kfdef.Name, err)
			}
		}
	}
	return nil
}

func removeCsv(c client.Client, r *rest.Config) error {
	// Get watchNamespace
	operatorNamespace, err := GetOperatorNamespace()
	if err != nil {
		return err
	}

	operatorCsv, err := getClusterServiceVersion(r, operatorNamespace)
	if err != nil {
		return err
	}

	if operatorCsv != nil {
		fmt.Printf("Deleting csv %s", operatorCsv.Name)
		err = c.Delete(context.TODO(), operatorCsv, []client.DeleteOption{}...)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("error deleting clusterserviceversion: %v", err)
		}
		fmt.Printf("Clusterserviceversion %s deleted as a part of uninstall.", operatorCsv.Name)
	}
	fmt.Printf("No clusterserviceversion for the operator found.")
	return nil
}

// getClusterServiceVersion retries the clusterserviceversions available in the operator namespace.
func getClusterServiceVersion(cfg *rest.Config, watchNameSpace string) (*ofapi.ClusterServiceVersion, error) {
	operatorClient, err := olmclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error getting operator client %v", err)
	}
	csvs, err := operatorClient.ClusterServiceVersions(watchNameSpace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// get csv with CRD DataScienceCluster
	if len(csvs.Items) != 0 {
		for _, csv := range csvs.Items {
			for _, operatorCR := range csv.Spec.CustomResourceDefinitions.Owned {
				if operatorCR.Kind == "DataScienceCluster" {
					return &csv, nil
				}
			}
		}
	}
	return nil, nil
}

func deleteResource(cli client.Client, namespace string, resourceType string) error {
	// In v2, Deployment selectors use a label "app.opendatahub.io/<componentName>" which is
	// not present in v1. Since label selectors are immutable, we need to delete the existing
	// deployments and recreated them.
	// because we can't proceed if a deployment is not deleted, we use exponential backoff
	// to retry the deletion until it succeeds
	var err error
	switch resourceType {
	case "deployment":
		err = wait.ExponentialBackoffWithContext(context.TODO(), wait.Backoff{
			// 5, 10, ,20, 40 then timeout
			Duration: 5 * time.Second,
			Factor:   2.0,
			Jitter:   0.1,
			Steps:    4,
			Cap:      1 * time.Minute,
		}, func(ctx context.Context) (bool, error) {
			done, err := deleteDeploymentsAndCheck(ctx, cli, namespace)
			return done, err
		})
	case "statefulset":
		err = wait.ExponentialBackoffWithContext(context.TODO(), wait.Backoff{
			// 10, 20 then timeout
			Duration: 10 * time.Second,
			Factor:   2.0,
			Jitter:   0.1,
			Steps:    2,
			Cap:      1 * time.Minute,
		}, func(ctx context.Context) (bool, error) {
			done, err := deleteStatefulsetsAndCheck(ctx, cli, namespace)
			return done, err
		})
	}
	return err
}

func deleteDeploymentsAndCheck(ctx context.Context, cli client.Client, namespace string) (bool, error) { //nolint
	// Delete Deployment objects
	var multiErr *multierror.Error
	deployments := &appsv1.DeploymentList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	if err := cli.List(ctx, deployments, listOpts); err != nil {
		return false, nil
	}
	// filter deployment which has the new label to limit that we do not over kill other deployment
	// this logic can be used even when upgrade from v2.4 to v2.5 without remove it
	markedForDeletion := []appsv1.Deployment{}
	for _, deployment := range deployments.Items {
		v2 := false
		selectorLabels := deployment.Spec.Selector.MatchLabels
		for label := range selectorLabels {
			if strings.Contains(label, "app.opendatahub.io/") {
				// this deployment has the new label, this is a v2 to v2 upgrade
				// there is no need to recreate it, as labels are matching
				v2 = true
				continue
			}
		}
		if !v2 {
			markedForDeletion = append(markedForDeletion, deployment)
			multiErr = multierror.Append(multiErr, cli.Delete(ctx, &deployment))
		}
	}

	for _, deployment := range markedForDeletion {
		if e := cli.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      deployment.Name,
		}, &deployment); e != nil {
			if apierrs.IsNotFound(e) {
				// resource has been successfully deleted
				continue
			} else {
				// unexpected error, report it
				multiErr = multierror.Append(multiErr, e)
			}
		} else {
			// resource still exists, wait for it to be deleted
			return false, nil
		}
	}

	return true, multiErr.ErrorOrNil()
}

func deleteStatefulsetsAndCheck(ctx context.Context, cli client.Client, namespace string) (bool, error) { //nolint
	// Delete statefulset objects
	var multiErr *multierror.Error
	statefulsets := &appsv1.StatefulSetList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	if err := cli.List(ctx, statefulsets, listOpts); err != nil {
		return false, nil
	}

	// even only we have one item to delete to avoid nil point still use range
	markedForDeletion := []appsv1.StatefulSet{}
	for _, statefulset := range statefulsets.Items {
		v2 := false
		selectorLabels := statefulset.Spec.Selector.MatchLabels
		for label := range selectorLabels {
			if strings.Contains(label, "app.opendatahub.io/") {
				v2 = true
				continue
			}
		}
		if !v2 {
			markedForDeletion = append(markedForDeletion, statefulset)
			multiErr = multierror.Append(multiErr, cli.Delete(ctx, &statefulset))
		}
	}

	for _, statefulset := range markedForDeletion {
		if e := cli.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      statefulset.Name,
		}, &statefulset); e != nil {
			if apierrs.IsNotFound(e) {
				// resource has been successfully deleted
				continue
			} else {
				// unexpected error, report it
				multiErr = multierror.Append(multiErr, e)
			}
		} else {
			// resource still exists, wait for it to be deleted
			return false, nil
		}
	}

	return true, multiErr.ErrorOrNil()
}
