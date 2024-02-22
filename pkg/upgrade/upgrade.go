package upgrade

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	// DeleteConfigMapLabel is the label for configMap used to trigger operator uninstall
	// TODO: Label should be updated if addon name changes.
	DeleteConfigMapLabel = "api.openshift.com/addon-managed-odh-delete"
)

// OperatorUninstall deletes all the externally generated resources. This includes monitoring resources and applications
// installed by KfDef.
func OperatorUninstall(cli client.Client, cfg *rest.Config) error {
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	if err := RemoveKfDefInstances(cli, platform); err != nil {
		return err
	}

	if err := removeDSCInitialization(cli); err != nil {
		return err
	}

	// Delete generated namespaces by the operator
	generatedNamespaces := &corev1.NamespaceList{}
	nsOptions := []client.ListOption{
		client.MatchingLabels{cluster.ODHGeneratedNamespaceLabel: "true"},
	}
	if err := cli.List(context.TODO(), generatedNamespaces, nsOptions...); err != nil {
		return fmt.Errorf("error getting generated namespaces : %w", err)
	}

	// Return if any one of the namespaces is Terminating due to resources that are in process of deletion. (e.g. CRDs)
	for _, namespace := range generatedNamespaces.Items {
		if namespace.Status.Phase == corev1.NamespaceTerminating {
			return fmt.Errorf("waiting for namespace %v to be deleted", namespace.Name)
		}
	}

	for _, namespace := range generatedNamespaces.Items {
		namespace := namespace
		if namespace.Status.Phase == corev1.NamespaceActive {
			if err := cli.Delete(context.TODO(), &namespace, []client.DeleteOption{}...); err != nil {
				return fmt.Errorf("error deleting namespace %v: %w", namespace.Name, err)
			}
			fmt.Printf("Namespace %s deleted as a part of uninstallation.\n", namespace.Name)
		}
	}

	// give enough time for namespace deletion before proceed
	time.Sleep(10 * time.Second)

	// We can only assume the subscription is using standard names
	// if user install by creating different named subs, then we will not know the name
	// we cannot remove CSV before remove subscription because that need SA account
	operatorNs, err := GetOperatorNamespace()
	if err != nil {
		return err
	}
	fmt.Printf("Removing operator subscription which in turn will remove installplan\n")
	subsName := "opendatahub-operator"
	if platform == deploy.SelfManagedRhods {
		subsName = "rhods-operator"
	} else if platform == deploy.ManagedRhods {
		subsName = "addon-managed-odh"
	}
	sub, _ := deploy.SubscriptionExists(cli, operatorNs, subsName)
	if sub == nil {
		fmt.Printf("Could not find subscription %s in namespace %s. Maybe you have a different one", subsName, operatorNs)
	} else {
		if err := cli.Delete(context.TODO(), sub); err != nil {
			return fmt.Errorf("error deleting subscription %s: %w", sub.Name, err)
		}
	}

	fmt.Printf("Removing the operator CSV in turn remove operator deployment\n")
	time.Sleep(5 * time.Second)

	err = removeCSV(cli, cfg)

	fmt.Printf("All resources deleted as part of uninstall.")
	return err
}

func removeDSCInitialization(cli client.Client) error {
	instanceList := &dsci.DSCInitializationList{}

	if err := cli.List(context.TODO(), instanceList); err != nil {
		return err
	}

	var multiErr *multierror.Error
	for _, dsciInstance := range instanceList.Items {
		dsciInstance := dsciInstance
		if err := cli.Delete(context.TODO(), &dsciInstance); !apierrs.IsNotFound(err) {
			multiErr = multierror.Append(multiErr, err)
		}
	}

	return multiErr.ErrorOrNil()
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
func CreateDefaultDSC(cli client.Client, _ deploy.Platform) error {
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
					Component: components.Component{ManagementState: operatorv1.Managed},
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
				Kueue: kueue.Kueue{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
				TrustyAI: trustyai.TrustyAI{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				ModelRegistry: modelregistry.ModelRegistry{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
			},
		},
	}
	err := cli.Create(context.TODO(), releaseDataScienceCluster)
	switch {
	case err == nil:
		fmt.Printf("created DataScienceCluster resource\n")
	case apierrs.IsAlreadyExists(err):
		// Do not update the DSC if it already exists
		fmt.Printf("DataScienceCluster resource already exists. It will not be updated with default DSC.\n")
		return nil
	default:
		return fmt.Errorf("failed to create DataScienceCluster custom resource: %w", err)
	}

	return nil
}

// createDefaultDSCI creates a default instance of DSCI
// If there exists an instance already, it patches the DSCISpec with default values
// Note: DSCI CR modifcations are not supported, as it is the initial prereq setting for the components.
func CreateDefaultDSCI(cli client.Client, _ deploy.Platform, appNamespace, monNamespace string) error {
	defaultDsciSpec := &dsci.DSCInitializationSpec{
		ApplicationsNamespace: appNamespace,
		Monitoring: dsci.Monitoring{
			ManagementState: operatorv1.Managed,
			Namespace:       monNamespace,
		},
		ServiceMesh: infrav1.ServiceMeshSpec{
			ManagementState: "Managed",
			ControlPlane: infrav1.ControlPlaneSpec{
				Name:              "data-science-smcp",
				Namespace:         "istio-system",
				MetricsCollection: "Istio",
			},
		},
		TrustedCABundle: dsci.TrustedCABundleSpec{
			ManagementState: "Managed",
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

	instances := &dsci.DSCInitializationList{}
	if err := cli.List(context.TODO(), instances); err != nil {
		return err
	}

	switch {
	case len(instances.Items) > 1:
		fmt.Printf("only one instance of DSCInitialization object is allowed. Please delete other instances.\n")
		return nil
	case len(instances.Items) == 1:
		// Do not patch/update if DSCI already exists.
		fmt.Printf("DSCInitialization resource already exists. It will not be updated with default DSCI.")
		return nil
	case len(instances.Items) == 0:
		fmt.Printf("create default DSCI CR.")
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
		return RemoveKfDefInstances(cli, platform)
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
			}
			return fmt.Errorf("error retrieving kfdef CRD : %w", err)
		}

		// If KfDef Instances found, and no DSC instances are found in Self-managed, that means this is an upgrade path from
		// legacy version. Create a default DSC instance
		kfDefList := &kfdefv1.KfDefList{}
		err := cli.List(context.TODO(), kfDefList)
		if err != nil {
			if apierrs.IsNotFound(err) {
				// If no KfDefs, do nothing and return
				return nil
			}
			return fmt.Errorf("error getting kfdef instances: : %w", err)
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

func CleanupExistingResource(cli client.Client, platform deploy.Platform) error {
	var multiErr *multierror.Error
	montNamespace := "redhat-ods-monitoring"
	ctx := context.TODO()

	// Special Handling of cleanup of deprecated model monitoring stack
	if platform == deploy.ManagedRhods {
		deprecatedDeployments := []string{"rhods-prometheus-operator"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, montNamespace, deprecatedDeployments, &appsv1.DeploymentList{}))

		deprecatedStatefulsets := []string{"prometheus-rhods-model-monitoring"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, montNamespace, deprecatedStatefulsets, &appsv1.StatefulSetList{}))

		deprecatedServices := []string{"rhods-model-monitoring"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, montNamespace, deprecatedServices, &corev1.ServiceList{}))

		deprecatedRoutes := []string{"rhods-model-monitoring"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, montNamespace, deprecatedRoutes, &routev1.RouteList{}))

		deprecatedSecrets := []string{"rhods-monitoring-oauth-config"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, montNamespace, deprecatedSecrets, &corev1.SecretList{}))

		deprecatedClusterroles := []string{"rhods-namespace-read", "rhods-prometheus-operator"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, montNamespace, deprecatedClusterroles, &authv1.ClusterRoleList{}))

		deprecatedClusterrolebindings := []string{"rhods-namespace-read", "rhods-prometheus-operator"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, montNamespace, deprecatedClusterrolebindings, &authv1.ClusterRoleBindingList{}))

		deprecatedServiceAccounts := []string{"rhods-prometheus-operator"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, montNamespace, deprecatedServiceAccounts, &corev1.ServiceAccountList{}))

		deprecatedServicemonitors := []string{"modelmesh-federated-metrics"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedServiceMonitors(ctx, cli, montNamespace, deprecatedServicemonitors))
	}
	// common logic for both self-managed and managed
	deprecatedOperatorSM := []string{"rhods-monitor-federation2"}
	multiErr = multierror.Append(multiErr, deleteDeprecatedServiceMonitors(ctx, cli, montNamespace, deprecatedOperatorSM))

	return multiErr.ErrorOrNil()
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

func RemoveKfDefInstances(cli client.Client, _ deploy.Platform) error {
	// Check if kfdef are deployed
	kfdefCrd := &apiextv1.CustomResourceDefinition{}

	err := cli.Get(context.TODO(), client.ObjectKey{Name: "kfdefs.kfdef.apps.kubeflow.org"}, kfdefCrd)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// If no Crd found, return, since its a new Installation
			return nil
		}
		return fmt.Errorf("error retrieving kfdef CRD : %w", err)
	}
	expectedKfDefList := &kfdefv1.KfDefList{}
	err = cli.List(context.TODO(), expectedKfDefList)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// If no KfDefs, do nothing and return
			return nil
		}
		return fmt.Errorf("error getting list of kfdefs: %w", err)
	}
	// Delete kfdefs
	for _, kfdef := range expectedKfDefList.Items {
		kfdef := kfdef
		// Remove finalizer
		updatedKfDef := &kfdef
		updatedKfDef.Finalizers = []string{}
		err = cli.Update(context.TODO(), updatedKfDef)
		if err != nil {
			return fmt.Errorf("error removing finalizers from kfdef %v : %w", kfdef.Name, err)
		}
		err = cli.Delete(context.TODO(), updatedKfDef)
		if err != nil {
			return fmt.Errorf("error deleting kfdef %v : %w", kfdef.Name, err)
		}
	}
	return nil
}

func removeCSV(c client.Client, r *rest.Config) error {
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
		fmt.Printf("Deleting CSV %s\n", operatorCsv.Name)
		err = c.Delete(context.TODO(), operatorCsv, []client.DeleteOption{}...)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return nil
			}

			return fmt.Errorf("error deleting clusterserviceversion: %w", err)
		}
		fmt.Printf("Clusterserviceversion %s deleted as a part of uninstall.\n", operatorCsv.Name)
		return nil
	}
	fmt.Printf("No clusterserviceversion for the operator found.\n")
	return nil
}

// getClusterServiceVersion retries the clusterserviceversions available in the operator namespace.
func getClusterServiceVersion(cfg *rest.Config, watchNameSpace string) (*ofapi.ClusterServiceVersion, error) {
	operatorClient, err := olmclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error getting operator client %w", err)
	}
	csvs, err := operatorClient.ClusterServiceVersions(watchNameSpace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// get CSV with CRD DataScienceCluster
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

func deleteDeprecatedResources(ctx context.Context, cli client.Client, namespace string, resourceList []string, resourceType client.ObjectList) error { //nolint:unparam
	var multiErr *multierror.Error
	listOpts := &client.ListOptions{Namespace: namespace}
	if err := cli.List(ctx, resourceType, listOpts); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}
	items := reflect.ValueOf(resourceType).Elem().FieldByName("Items")
	for i := 0; i < items.Len(); i++ {
		item := items.Index(i).Addr().Interface().(client.Object) //nolint:errcheck,forcetypeassert
		for _, name := range resourceList {
			if name == item.GetName() {
				fmt.Printf("Attempting to delete %s in namespace %s\n", item.GetName(), namespace)
				err := cli.Delete(ctx, item)
				if err != nil {
					if apierrs.IsNotFound(err) {
						fmt.Printf("Could not find %s in namespace %s\n", item.GetName(), namespace)
					} else {
						multiErr = multierror.Append(multiErr, err)
					}
				}
				fmt.Printf("Successfully deleted %s\n", item.GetName())
			}
		}
	}
	return multiErr.ErrorOrNil()
}

// Need to handle ServiceMonitor deletion separately as the generic function does not work for ServiceMonitors because of how the package is built.
func deleteDeprecatedServiceMonitors(ctx context.Context, cli client.Client, namespace string, resourceList []string) error {
	var multiErr *multierror.Error
	listOpts := &client.ListOptions{Namespace: namespace}
	servicemonitors := &monitoringv1.ServiceMonitorList{}
	if err := cli.List(ctx, servicemonitors, listOpts); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}

	for _, servicemonitor := range servicemonitors.Items {
		servicemonitor := servicemonitor
		for _, name := range resourceList {
			if name == servicemonitor.Name {
				fmt.Printf("Attempting to delete %s in namespace %s\n", servicemonitor.Name, namespace)
				err := cli.Delete(ctx, servicemonitor)
				if err != nil {
					if apierrs.IsNotFound(err) {
						fmt.Printf("Could not find %s in namespace %s\n", servicemonitor.Name, namespace)
					} else {
						multiErr = multierror.Append(multiErr, err)
					}
				}
				fmt.Printf("Successfully deleted %s\n", servicemonitor.Name)
			}
		}
	}
	return multiErr.ErrorOrNil()
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

func deleteDeploymentsAndCheck(ctx context.Context, cli client.Client, namespace string) (bool, error) {
	// Delete Deployment objects
	var multiErr *multierror.Error
	deployments := &appsv1.DeploymentList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	if err := cli.List(ctx, deployments, listOpts); err != nil {
		return false, nil //nolint:nilerr
	}
	// filter deployment which has the new label to limit that we do not overkill other deployment
	// this logic can be used even when upgrade from v2.4 to v2.5 without remove it
	markedForDeletion := []appsv1.Deployment{}
	for _, deployment := range deployments.Items {
		deployment := deployment
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
		deployment := deployment
		if e := cli.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      deployment.Name,
		}, &deployment); e != nil {
			if apierrs.IsNotFound(e) {
				// resource has been successfully deleted
				continue
			}
			// unexpected error, report it
			multiErr = multierror.Append(multiErr, e) //nolint:staticcheck,wastedassign
		}
		// resource still exists, wait for it to be deleted
		return false, nil
	}

	return true, multiErr.ErrorOrNil()
}

func deleteStatefulsetsAndCheck(ctx context.Context, cli client.Client, namespace string) (bool, error) {
	// Delete statefulset objects
	var multiErr *multierror.Error
	statefulsets := &appsv1.StatefulSetList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	if err := cli.List(ctx, statefulsets, listOpts); err != nil {
		return false, nil //nolint:nilerr
	}

	// even only we have one item to delete to avoid nil point still use range
	markedForDeletion := []appsv1.StatefulSet{}
	for _, statefulset := range statefulsets.Items {
		v2 := false
		statefulset := statefulset
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
		statefulset := statefulset
		if e := cli.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      statefulset.Name,
		}, &statefulset); e != nil {
			if apierrs.IsNotFound(e) {
				// resource has been successfully deleted
				continue
			}
			// unexpected error, report it
			multiErr = multierror.Append(multiErr, e)
		} else {
			// resource still exists, wait for it to be deleted
			return false, nil
		}
	}

	return true, multiErr.ErrorOrNil()
}
