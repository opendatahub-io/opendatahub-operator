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
	rbacv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
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
	releaseDataScienceCluster := &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: "datasciencecluster.opendatahub.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsc",
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
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
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				TrainingOperator: trainingoperator.TrainingOperator{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
			},
		},
	}
	err := cluster.CreateWithRetry(ctx, cli, releaseDataScienceCluster, 1) // 1 min timeout
	if err != nil {
		return fmt.Errorf("failed to create DataScienceCluster custom resource: %w", err)
	}
	return nil
}

// CreateDefaultDSCI creates a default instance of DSCI
// If there exists default-dsci instance already, it will not update DSCISpec on it.
// Note: DSCI CR modifcations are not supported, as it is the initial prereq setting for the components.
func CreateDefaultDSCI(ctx context.Context, cli client.Client, _ cluster.Platform, appNamespace, monNamespace string) error {
	defaultDsciSpec := &dsciv1.DSCInitializationSpec{
		ApplicationsNamespace: appNamespace,
		Monitoring: dsciv1.Monitoring{
			ManagementState: operatorv1.Managed,
			Namespace:       monNamespace,
		},
		ServiceMesh: &infrav1.ServiceMeshSpec{
			ManagementState: "Managed",
			ControlPlane: infrav1.ControlPlaneSpec{
				Name:              "data-science-smcp",
				Namespace:         "istio-system",
				MetricsCollection: "Istio",
			},
		},
		TrustedCABundle: &dsciv1.TrustedCABundleSpec{
			ManagementState: "Managed",
		},
	}

	defaultDsci := &dsciv1.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "dscinitialization.opendatahub.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: *defaultDsciSpec,
	}

	instances := &dsciv1.DSCInitializationList{}
	if err := cli.List(ctx, instances); err != nil {
		return err
	}

	switch {
	case len(instances.Items) > 1:
		ctrl.Log.Info("only one instance of DSCInitialization object is allowed. Please delete other instances.")
		return nil
	case len(instances.Items) == 1:
		// Do not patch/update if DSCI already exists.
		ctrl.Log.Info("DSCInitialization resource already exists. It will not be updated with default DSCI.")
		return nil
	case len(instances.Items) == 0:
		ctrl.Log.Info("create default DSCI CR.")
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
func CleanupExistingResource(ctx context.Context,
	cli client.Client,
	platform cluster.Platform,
	dscApplicationsNamespace, dscMonitoringNamespace string,
	oldReleaseVersion cluster.Release,
) error {
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
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedClusterroles, &rbacv1.ClusterRoleList{}))

		deprecatedClusterrolebindings := []string{"rhods-namespace-read", "rhods-prometheus-operator"}
		multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscMonitoringNamespace, deprecatedClusterrolebindings, &rbacv1.ClusterRoleBindingList{}))

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

	// cleanup for github.com/opendatahub-io/pull/888
	deprecatedFeatureTrackers := []string{dscApplicationsNamespace + "-kserve-temporary-fixes"}
	multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, dscApplicationsNamespace, deprecatedFeatureTrackers, &featuresv1.FeatureTrackerList{}))

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
		if err := upgradeODCCR(ctx, cli, "odh-dashboard-config", dscApplicationsNamespace, oldReleaseVersion); err != nil {
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
			ctrl.Log.Info("CRD not found, will not delete " + res.Gvk.String())
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
				ctrl.Log.Info("Deleted object " + item.GetName() + " " + res.Gvk.String() + "in namespace" + res.Namespace)
			}
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
				ctrl.Log.Info("Attempting to delete " + item.GetName() + " in namespace " + namespace)
				err := cli.Delete(ctx, item)
				if err != nil {
					if k8serr.IsNotFound(err) {
						ctrl.Log.Info("Could not find " + item.GetName() + " in namespace " + namespace)
					} else {
						multiErr = multierror.Append(multiErr, err)
					}
				}
				ctrl.Log.Info("Successfully deleted " + item.GetName())
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
				ctrl.Log.Info("Attempting to delete " + servicemonitor.Name + " in namespace " + namespace)
				err := cli.Delete(ctx, servicemonitor)
				if err != nil {
					if k8serr.IsNotFound(err) {
						ctrl.Log.Info("Could not find " + servicemonitor.Name + " in namespace " + namespace)
					} else {
						multiErr = multierror.Append(multiErr, err)
					}
				}
				ctrl.Log.Info("Successfully deleted " + servicemonitor.Name)
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

// upgradODCCR handles different cases:
// 1. unset ownerreference for CR odh-dashboard-config
// 2. flip TrustyAI BiasMetrics to false (.spec.dashboardConfig.disableBiasMetrics) if it is lower release version than input 'release'.
// 3. flip ModelRegistry to false (.spec.dashboardConfig.disableModelRegistry) if it is lower release version than input 'release'.
func upgradeODCCR(ctx context.Context, cli client.Client, instanceName string, applicationNS string, release cluster.Release) error {
	crd := &apiextv1.CustomResourceDefinition{}
	if err := cli.Get(ctx, client.ObjectKey{Name: "odhdashboardconfigs.opendatahub.io"}, crd); err != nil {
		return client.IgnoreNotFound(err)
	}
	odhObject := &unstructured.Unstructured{}
	odhObject.SetGroupVersionKind(gvk.OdhDashboardConfig)
	if err := cli.Get(ctx, client.ObjectKey{
		Namespace: applicationNS,
		Name:      instanceName,
	}, odhObject); err != nil {
		return client.IgnoreNotFound(err)
	}

	if err := unsetOwnerReference(ctx, cli, instanceName, odhObject); err != nil {
		return err
	}

	if err := updateODCBiasMetrics(ctx, cli, instanceName, release, odhObject); err != nil {
		return err
	}

	return updateODCModelRegistry(ctx, cli, instanceName, release, odhObject)
}

func unsetOwnerReference(ctx context.Context, cli client.Client, instanceName string, odhObject *unstructured.Unstructured) error {
	if odhObject.GetOwnerReferences() != nil {
		// set to nil as updates
		odhObject.SetOwnerReferences(nil)
		if err := cli.Update(ctx, odhObject); err != nil {
			return fmt.Errorf("error unset ownerreference for CR %s : %w", instanceName, err)
		}
	}
	return nil
}

func updateODCBiasMetrics(ctx context.Context, cli client.Client, instanceName string, oldRelease cluster.Release, odhObject *unstructured.Unstructured) error {
	// "from version" as oldRelease, if return "0.0.0" meaning running on 2.10- release/dummy CI build
	// if oldRelease is lower than 2.14.0(e.g 2.13.x-a), flip disableBiasMetrics to false (even the field did not exist)
	if oldRelease.Version.Minor < 14 {
		ctrl.Log.Info("Upgrade force BiasMetrics to false in " + instanceName + " CR due to old release < 2.14.0")
		// flip TrustyAI BiasMetrics to false (.spec.dashboardConfig.disableBiasMetrics)
		disableBiasMetricsValue := []byte(`{"spec": {"dashboardConfig": {"disableBiasMetrics": false}}}`)
		if err := cli.Patch(ctx, odhObject, client.RawPatch(types.MergePatchType, disableBiasMetricsValue)); err != nil {
			return fmt.Errorf("error enable BiasMetrics in CR %s : %w", instanceName, err)
		}
		return nil
	}
	ctrl.Log.Info("Upgrade does not force BiasMetrics to false due to from release >= 2.14.0")
	return nil
}

func updateODCModelRegistry(ctx context.Context, cli client.Client, instanceName string, oldRelease cluster.Release, odhObject *unstructured.Unstructured) error {
	// "from version" as oldRelease, if return "0.0.0" meaning running on 2.10- release/dummy CI build
	// if oldRelease is lower than 2.14.0(e.g 2.13.x-a), flip disableModelRegistry to false (even the field did not exist)
	if oldRelease.Version.Minor < 14 {
		ctrl.Log.Info("Upgrade force ModelRegistry to false in " + instanceName + " CR due to old release < 2.14.0")
		disableModelRegistryValue := []byte(`{"spec": {"dashboardConfig": {"disableModelRegistry": false}}}`)
		if err := cli.Patch(ctx, odhObject, client.RawPatch(types.MergePatchType, disableModelRegistryValue)); err != nil {
			return fmt.Errorf("error enable ModelRegistry in CR %s : %w", instanceName, err)
		}
		return nil
	}
	ctrl.Log.Info("Upgrade does not force ModelRegistry to false due to from release >= 2.14.0")
	return nil
}

func RemoveLabel(ctx context.Context, cli client.Client, objectName string, labelKey string) error {
	foundNamespace := &corev1.Namespace{}
	if err := cli.Get(ctx, client.ObjectKey{Name: objectName}, foundNamespace); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("could not get %s namespace: %w", objectName, err)
	}
	delete(foundNamespace.Labels, labelKey)
	if err := cli.Update(ctx, foundNamespace); err != nil {
		return fmt.Errorf("error removing %s from %s : %w", labelKey, objectName, err)
	}
	return nil
}

func deleteDeprecatedNamespace(ctx context.Context, cli client.Client, namespace string) error {
	foundNamespace := &corev1.Namespace{}
	if err := cli.Get(ctx, client.ObjectKey{Name: namespace}, foundNamespace); err != nil {
		if k8serr.IsNotFound(err) {
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
		ctrl.Log.Info("Skip deletion of namespace " + namespace + " due to running Pods in it")
		return nil
	}

	// Delete namespace if no pods found
	if err := cli.Delete(ctx, foundNamespace); err != nil {
		return fmt.Errorf("could not delete %s namespace: %w", namespace, err)
	}

	return nil
}

func GetDeployedRelease(ctx context.Context, cli client.Client) (cluster.Release, error) {
	dsciInstance := &dsciv1.DSCInitializationList{}
	if err := cli.List(ctx, dsciInstance); err != nil {
		return cluster.Release{}, err
	}
	if len(dsciInstance.Items) == 1 { // found one DSCI CR found
		// can return a valid Release or 0.0.0
		return dsciInstance.Items[0].Status.Release, nil
	}
	// no DSCI CR found, try with DSC CR
	dscInstances := &dscv1.DataScienceClusterList{}
	if err := cli.List(ctx, dscInstances); err != nil {
		return cluster.Release{}, err
	}
	if len(dscInstances.Items) == 1 { // one DSC CR found
		return dscInstances.Items[0].Status.Release, nil
	}
	// could be a clean installation or both CRs are deleted already
	return cluster.Release{}, nil
}
