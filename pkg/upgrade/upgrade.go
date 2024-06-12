// Package upgrade provides functions of upgrade ODH from v1 to v2 and vaiours v2 versions.
// It contains both the logic to upgrade the ODH components and the logic to cleanup the deprecated resources.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfdefv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trainingoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

type ResourceSpec struct {
	Gvk       schema.GroupVersionKind
	Namespace string
	// path to the field, like "metadata", "name"
	Path []string
	// set of values for the field to match object, any one matches
	Values []string
}

// CreateDefaultDSC creates a default instance of DSC.
// Note: When the platform is not Managed, and a DSC instance already exists, the function doesn't re-create/update the resource.
func CreateDefaultDSC(ctx context.Context, cli client.Client) error {
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
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				Ray: ray.Ray{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				Kueue: kueue.Kueue{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				TrustyAI: trustyai.TrustyAI{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				ModelRegistry: modelregistry.ModelRegistry{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
				TrainingOperator: trainingoperator.TrainingOperator{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
			},
		},
	}
	err := cluster.CreateWithRetry(ctx, cli, releaseDataScienceCluster, 1) // 1 min timeout
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
func CreateDefaultDSCI(ctx context.Context, cli client.Client, _ cluster.Platform, appNamespace, monNamespace string) error {
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
	if err := cli.List(ctx, instances); err != nil {
		return err
	}

	switch {
	case len(instances.Items) > 1:
		fmt.Println("only one instance of DSCInitialization object is allowed. Please delete other instances.")
		return nil
	case len(instances.Items) == 1:
		// Do not patch/update if DSCI already exists.
		fmt.Println("DSCInitialization resource already exists. It will not be updated with default DSCI.")
		return nil
	case len(instances.Items) == 0:
		fmt.Println("create default DSCI CR.")
		err := cluster.CreateWithRetry(ctx, cli, defaultDsci, 1) // 1 min timeout
		if err != nil {
			return err
		}
	}
	return nil
}

func getJPHOdhDocumentResources(namespace string, matchedName []string) []ResourceSpec {
	metadataName := []string{"metadata", "name"}
	return []ResourceSpec{
		{
			Gvk:       gvk.OdhDocument,
			Namespace: namespace,
			Path:      metadataName,
			Values:    matchedName,
		},
	}
}

func getDashboardWatsonResources(ns string) []ResourceSpec {
	metadataName := []string{"metadata", "name"}
	specAppName := []string{"spec", "appName"}
	appName := []string{"watson-studio"}

	return []ResourceSpec{
		{
			Gvk:       gvk.OdhQuickStart,
			Namespace: ns,
			Path:      specAppName,
			Values:    appName,
		},
		{
			Gvk:       gvk.OdhDocument,
			Namespace: ns,
			Path:      specAppName,
			Values:    appName,
		},
		{
			Gvk:       gvk.OdhApplication,
			Namespace: ns,
			Path:      metadataName,
			Values:    appName,
		},
	}
}

// TODO: remove function once we have a generic solution across all components.
func CleanupExistingResource(ctx context.Context, cli client.Client, platform cluster.Platform, dscApplicationsNamespace, dscMonitoringNamespace string) error {
	var multiErr *multierror.Error
	// Special Handling of cleanup of deprecated model monitoring stack
	if platform == cluster.ManagedRhods {
		deprecatedDeployments := []string{"rhods-prometheus-operator"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedDeployments, &appsv1.DeploymentList{}))

		deprecatedStatefulsets := []string{"prometheus-rhods-model-monitoring"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedStatefulsets, &appsv1.StatefulSetList{}))

		deprecatedServices := []string{"rhods-model-monitoring"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedServices, &corev1.ServiceList{}))

		deprecatedRoutes := []string{"rhods-model-monitoring"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedRoutes, &routev1.RouteList{}))

		deprecatedSecrets := []string{"rhods-monitoring-oauth-config"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedSecrets, &corev1.SecretList{}))

		deprecatedClusterroles := []string{"rhods-namespace-read", "rhods-prometheus-operator"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedClusterroles, &authv1.ClusterRoleList{}))

		deprecatedClusterrolebindings := []string{"rhods-namespace-read", "rhods-prometheus-operator"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedClusterrolebindings, &authv1.ClusterRoleBindingList{}))

		deprecatedServiceAccounts := []string{"rhods-prometheus-operator"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedServiceAccounts, &corev1.ServiceAccountList{}))

		deprecatedServicemonitors := []string{"modelmesh-federated-metrics"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedServiceMonitors(ctx, cli, dscMonitoringNamespace, deprecatedServicemonitors))
	}
	// common logic for both self-managed and managed
	deprecatedOperatorSM := []string{"rhods-monitor-federation2"}
	multiErr = multierror.Append(multiErr, deleteDeprecatedServiceMonitors(ctx, cli, dscMonitoringNamespace, deprecatedOperatorSM))

	// Remove deprecated opendatahub namespace(previously owned by kuberay and Kueue)
	multiErr = multierror.Append(multiErr, deleteDeprecatedNamespace(ctx, cli, "opendatahub"))

	// Handling for dashboard OdhApplication Jupyterhub CR, see jira #443
	multiErr = multierror.Append(multiErr, removOdhApplicationsCR(ctx, cli, gvk.OdhApplication, "jupyterhub", dscApplicationsNamespace))

	// Handling for dashboard OdhDocument Jupyterhub CR, see jira #443 comments
	odhDocJPH := getJPHOdhDocumentResources(
		dscApplicationsNamespace,
		[]string{
			"jupyterhub-install-python-packages",
			"jupyterhub-update-server-settings",
			"jupyterhub-view-installed-packages",
			"jupyterhub-use-s3-bucket-data",
		})
	multiErr = multierror.Append(multiErr, deleteResources(ctx, cli, &odhDocJPH))
	// only apply on RHOAI since ODH has a different way to create this CR by dashboard
	if platform == cluster.SelfManagedRhods || platform == cluster.ManagedRhods {
		if err := unsetOwnerReference(cli, "odh-dashboard-config", dscApplicationsNamespace); err != nil {
			return err
		}
	}

	// to take a reference
	toDelete := getDashboardWatsonResources(dscApplicationsNamespace)
	multiErr = multierror.Append(multiErr, deleteResources(ctx, cli, &toDelete))

	return multiErr.ErrorOrNil()
}

func deleteResources(ctx context.Context, c client.Client, resources *[]ResourceSpec) error {
	var errors *multierror.Error

	for _, res := range *resources {
		err := deleteOneResource(ctx, c, res)
		errors = multierror.Append(errors, err)
	}

	return errors.ErrorOrNil()
}

func deleteOneResource(ctx context.Context, c client.Client, res ResourceSpec) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(res.Gvk)

	err := c.List(ctx, list, client.InNamespace(res.Namespace))
	if err != nil {
		if errors.Is(err, &meta.NoKindMatchError{}) {
			fmt.Printf("Could not delete %v: CRD not found\n", res.Gvk)
			return nil
		}
		return fmt.Errorf("failed to list %s: %w", res.Gvk.Kind, err)
	}

	for _, item := range list.Items {
		item := item
		v, ok, err := unstructured.NestedString(item.Object, res.Path...)
		if err != nil {
			return fmt.Errorf("failed to get field %v for %s %s/%s: %w", res.Path, res.Gvk.Kind, res.Namespace, item.GetName(), err)
		}

		if !ok {
			return fmt.Errorf("unexisting path to delete: %v", res.Path)
		}

		for _, toDelete := range res.Values {
			if v == toDelete {
				err = c.Delete(ctx, &item)
				if err != nil {
					return fmt.Errorf("failed to delete %s %s/%s: %w", res.Gvk.Kind, res.Namespace, item.GetName(), err)
				}
				fmt.Println("Deleted object", item.GetName(), res.Gvk, "in namespace", res.Namespace)
			}
		}
	}

	return nil
}

func RemoveKfDefInstances(ctx context.Context, cli client.Client) error {
	// Check if kfdef are deployed
	kfdefCrd := &apiextv1.CustomResourceDefinition{}

	err := cli.Get(ctx, client.ObjectKey{Name: "kfdefs.kfdef.apps.kubeflow.org"}, kfdefCrd)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// If no Crd found, return, since its a new Installation
			return nil
		}
		return fmt.Errorf("error retrieving kfdef CRD : %w", err)
	}
	expectedKfDefList := &kfdefv1.KfDefList{}
	err = cli.List(ctx, expectedKfDefList)
	if err != nil {
		return fmt.Errorf("error getting list of kfdefs: %w", err)
	}
	// Delete kfdefs
	for _, kfdef := range expectedKfDefList.Items {
		kfdef := kfdef
		// Remove finalizer
		updatedKfDef := &kfdef
		updatedKfDef.Finalizers = []string{}
		err = cli.Update(ctx, updatedKfDef)
		if err != nil {
			return fmt.Errorf("error removing finalizers from kfdef %v : %w", kfdef.Name, err)
		}
		err = cli.Delete(ctx, updatedKfDef)
		if err != nil {
			return fmt.Errorf("error deleting kfdef %v : %w", kfdef.Name, err)
		}
	}
	return nil
}

func deleteDeprecatedResources(ctx context.Context, cli client.Client, namespace string, resourceList []string, resourceType client.ObjectList) error {
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

func removOdhApplicationsCR(ctx context.Context, cli client.Client, gvk schema.GroupVersionKind, instanceName string, applicationNS string) error {
	// first check if CRD in cluster
	crd := &apiextv1.CustomResourceDefinition{}
	if err := cli.Get(ctx, client.ObjectKey{Name: "odhapplications.dashboard.opendatahub.io"}, crd); err != nil {
		return client.IgnoreNotFound(err)
	}

	// then check if CR in cluster to delete
	odhObject := &unstructured.Unstructured{}
	odhObject.SetGroupVersionKind(gvk)
	if err := cli.Get(ctx, client.ObjectKey{
		Namespace: applicationNS,
		Name:      instanceName,
	}, odhObject); err != nil {
		return client.IgnoreNotFound(err)
	}
	if err := cli.Delete(ctx, odhObject); err != nil {
		return fmt.Errorf("error deleting CR %s : %w", instanceName, err)
	}

	return nil
}

func unsetOwnerReference(cli client.Client, instanceName string, applicationNS string) error {
	crd := &apiextv1.CustomResourceDefinition{}
	if err := cli.Get(context.TODO(), client.ObjectKey{Name: "odhdashboardconfigs.opendatahub.io"}, crd); err != nil {
		return client.IgnoreNotFound(err)
	}
	odhObject := &unstructured.Unstructured{}
	odhObject.SetGroupVersionKind(gvk.OdhDashboardConfig)
	if err := cli.Get(context.TODO(), client.ObjectKey{
		Namespace: applicationNS,
		Name:      instanceName,
	}, odhObject); err != nil {
		return client.IgnoreNotFound(err)
	}
	if odhObject.GetOwnerReferences() != nil {
		// set to nil as updates
		odhObject.SetOwnerReferences(nil)
		if err := cli.Update(context.TODO(), odhObject); err != nil {
			return fmt.Errorf("error unset ownerreference for CR %s : %w", instanceName, err)
		}
	}
	return nil
}

func RemoveLabel(cli client.Client, objectName string, labelKey string) error {
	foundNamespace := &corev1.Namespace{}
	if err := cli.Get(context.TODO(), client.ObjectKey{Name: objectName}, foundNamespace); err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("could not get %s namespace: %w", objectName, err)
	}
	delete(foundNamespace.Labels, labelKey)
	if err := cli.Update(context.TODO(), foundNamespace); err != nil {
		return fmt.Errorf("error removing %s from %s : %w", labelKey, objectName, err)
	}
	return nil
}

func deleteDeprecatedNamespace(ctx context.Context, cli client.Client, namespace string) error {
	foundNamespace := &corev1.Namespace{}
	if err := cli.Get(ctx, client.ObjectKey{Name: namespace}, foundNamespace); err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("could not get %s namespace: %w", namespace, err)
	}

	// Check if namespace is owned by DSC
	isOwnedByDSC := false
	for _, owner := range foundNamespace.OwnerReferences {
		if owner.Kind == "DataScienceCluster" {
			isOwnedByDSC = true
		}
	}
	if !isOwnedByDSC {
		return nil
	}

	// Check if namespace has pods running
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}
	if err := cli.List(ctx, podList, listOpts...); err != nil {
		return fmt.Errorf("error getting pods from namespace %s: %w", namespace, err)
	}
	if len(podList.Items) != 0 {
		fmt.Printf("Skip deletion of namespace %s due to running Pods in it\n", namespace)
		return nil
	}

	// Delete namespace if no pods found
	if err := cli.Delete(ctx, foundNamespace); err != nil {
		return fmt.Errorf("could not delete %s namespace: %w", namespace, err)
	}

	return nil
}
