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
	templatev1 "github.com/openshift/api/template/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
var (
	NotebookSizesData = []any{
		map[string]any{
			"name": "Small",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "1",
					"memory": "8Gi",
				},
				"limits": map[string]any{
					"cpu":    "2",
					"memory": "8Gi",
				},
			},
		},
		map[string]any{
			"name": "Medium",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "3",
					"memory": "24Gi",
				},
				"limits": map[string]any{
					"cpu":    "6",
					"memory": "24Gi",
				},
			},
		},
		map[string]any{
			"name": "Large",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "7",
					"memory": "56Gi",
				},
				"limits": map[string]any{
					"cpu":    "14",
					"memory": "56Gi",
				},
			},
		},
		map[string]any{
			"name": "X Large",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "15",
					"memory": "120Gi",
				},
				"limits": map[string]any{
					"cpu":    "30",
					"memory": "120Gi",
				},
			},
		},
	}
	ModelServerSizeData = []any{
		map[string]any{
			"name": "Small",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "1",
					"memory": "4Gi",
				},
				"limits": map[string]any{
					"cpu":    "2",
					"memory": "8Gi",
				},
			},
		},
		map[string]any{
			"name": "Medium",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "4",
					"memory": "8Gi",
				},
				"limits": map[string]any{
					"cpu":    "8",
					"memory": "10Gi",
				},
			},
		},
		map[string]any{
			"name": "Large",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "6",
					"memory": "16Gi",
				},
				"limits": map[string]any{
					"cpu":    "10",
					"memory": "20Gi",
				},
			},
		},
		map[string]any{
			"name": "Custom",
			"resources": map[string]any{
				"requests": map[string]any{},
				"limits":   map[string]any{},
			},
		},
	}
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
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				ModelMeshServing: componentApi.DSCModelMeshServing{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				DataSciencePipelines: componentApi.DSCDataSciencePipelines{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				CodeFlare: componentApi.DSCCodeFlare{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				Ray: componentApi.DSCRay{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				Kueue: componentApi.DSCKueue{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				TrustyAI: componentApi.DSCTrustyAI{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				ModelRegistry: componentApi.DSCModelRegistry{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
				},
				TrainingOperator: componentApi.DSCTrainingOperator{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
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
func CreateDefaultDSCI(ctx context.Context, cli client.Client, _ common.Platform, monNamespace string) error {
	log := logf.FromContext(ctx)
	defaultDsciSpec := &dsciv1.DSCInitializationSpec{
		Monitoring: serviceApi.DSCIMonitoring{
			ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
			MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
				Namespace: monNamespace,
			},
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
		log.Info("only one instance of DSCInitialization object is allowed. Please delete other instances.")
		return nil
	case len(instances.Items) == 1:
		// Do not patch/update if DSCI already exists.
		log.Info("DSCInitialization resource already exists. It will not be updated with default DSCI.")
		return nil
	case len(instances.Items) == 0:
		log.Info("create default DSCI CR.")
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
	platform common.Platform,
	oldReleaseVersion common.Release,
) error {
	var multiErr *multierror.Error
	// get DSCI CR to get application namespace
	dsciList := &dsciv1.DSCInitializationList{}
	if err := cli.List(ctx, dsciList); err != nil {
		return err
	}
	if len(dsciList.Items) == 0 {
		return nil
	}
	d := &dsciList.Items[0]
	// Handling for dashboard OdhApplication Jupyterhub CR, see jira #443
	multiErr = multierror.Append(multiErr, removOdhApplicationsCR(ctx, cli, gvk.OdhApplication, "jupyterhub", d.Spec.ApplicationsNamespace))

	// cleanup for github.com/opendatahub-io/pull/888
	deprecatedFeatureTrackers := []string{d.Spec.ApplicationsNamespace + "-kserve-temporary-fixes"}
	multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, d.Spec.ApplicationsNamespace, deprecatedFeatureTrackers, &featuresv1.FeatureTrackerList{}))

	// Cleanup of deprecated default RoleBinding resources
	deprecatedDefaultRoleBinding := []string{d.Spec.ApplicationsNamespace}
	multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, d.Spec.ApplicationsNamespace, deprecatedDefaultRoleBinding, &rbacv1.RoleBindingList{}))

	// Handling for dashboard OdhDocument Jupyterhub CR, see jira #443 comments
	odhDocJPH := getJPHOdhDocumentResources(
		d.Spec.ApplicationsNamespace,
		[]string{
			"jupyterhub-install-python-packages",
			"jupyterhub-update-server-settings",
			"jupyterhub-view-installed-packages",
			"jupyterhub-use-s3-bucket-data",
		})
	multiErr = multierror.Append(multiErr, deleteResources(ctx, cli, &odhDocJPH))
	// only apply on RHOAI since ODH has a different way to create this CR by dashboard
	if platform == cluster.SelfManagedRhoai || platform == cluster.ManagedRhoai {
		if err := upgradeODCCR(ctx, cli, "odh-dashboard-config", d.Spec.ApplicationsNamespace, oldReleaseVersion); err != nil {
			return err
		}
	}
	// remove modelreg proxy container from deployment in ODH
	if platform == cluster.OpenDataHub {
		if err := removeRBACProxyModelRegistry(ctx, cli, "model-registry-operator", "kube-rbac-proxy", d.Spec.ApplicationsNamespace); err != nil {
			return err
		}
	}

	// to take a reference
	toDelete := getDashboardWatsonResources(d.Spec.ApplicationsNamespace)
	multiErr = multierror.Append(multiErr, deleteResources(ctx, cli, &toDelete))

	// cleanup nvidia nim integration
	multiErr = multierror.Append(multiErr, cleanupNimIntegration(ctx, cli, oldReleaseVersion, d.Spec.ApplicationsNamespace))
	// cleanup model controller legacy deployment
	multiErr = multierror.Append(multiErr, cleanupModelControllerLegacyDeployment(ctx, cli, d.Spec.ApplicationsNamespace))

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
	log := logf.FromContext(ctx)
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(res.Gvk)

	err := c.List(ctx, list, client.InNamespace(res.Namespace))
	if err != nil {
		if errors.Is(err, &meta.NoKindMatchError{}) {
			log.Info("CRD not found, will not delete " + res.Gvk.String())
			return nil
		}
		return fmt.Errorf("failed to list %s: %w", res.Gvk.Kind, err)
	}

	for _, item := range list.Items {
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
				log.Info("Deleted object " + item.GetName() + " " + res.Gvk.String() + "in namespace" + res.Namespace)
			}
		}
	}

	return nil
}

func deleteDeprecatedResources(ctx context.Context, cli client.Client, namespace string, resourceList []string, resourceType client.ObjectList) error {
	log := logf.FromContext(ctx)
	var multiErr *multierror.Error
	listOpts := &client.ListOptions{Namespace: namespace}
	if err := cli.List(ctx, resourceType, listOpts); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}
	items := reflect.ValueOf(resourceType).Elem().FieldByName("Items")
	for i := range items.Len() {
		item := items.Index(i).Addr().Interface().(client.Object) //nolint:errcheck,forcetypeassert
		for _, name := range resourceList {
			if name == item.GetName() {
				log.Info("Attempting to delete " + item.GetName() + " in namespace " + namespace)
				err := cli.Delete(ctx, item)
				if err != nil {
					if k8serr.IsNotFound(err) {
						log.Info("Could not find " + item.GetName() + " in namespace " + namespace)
					} else {
						multiErr = multierror.Append(multiErr, err)
					}
				}
				log.Info("Successfully deleted " + item.GetName())
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
func upgradeODCCR(ctx context.Context, cli client.Client, instanceName string, applicationNS string, release common.Release) error {
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

func updateODCBiasMetrics(ctx context.Context, cli client.Client, instanceName string, oldRelease common.Release, odhObject *unstructured.Unstructured) error {
	log := logf.FromContext(ctx)
	// "from version" as oldRelease, if return "0.0.0" meaning running on 2.10- release/dummy CI build
	// if oldRelease is lower than 2.14.0(e.g 2.13.x-a), flip disableBiasMetrics to false (even the field did not exist)
	if oldRelease.Version.Minor < 14 {
		log.Info("Upgrade force BiasMetrics to false in " + instanceName + " CR due to old release < 2.14.0")
		// flip TrustyAI BiasMetrics to false (.spec.dashboardConfig.disableBiasMetrics)
		disableBiasMetricsValue := []byte(`{"spec": {"dashboardConfig": {"disableBiasMetrics": false}}}`)
		if err := cli.Patch(ctx, odhObject, client.RawPatch(types.MergePatchType, disableBiasMetricsValue)); err != nil {
			return fmt.Errorf("error enable BiasMetrics in CR %s : %w", instanceName, err)
		}
		return nil
	}
	log.Info("Upgrade does not force BiasMetrics to false due to from release >= 2.14.0")
	return nil
}

func updateODCModelRegistry(ctx context.Context, cli client.Client, instanceName string, oldRelease common.Release, odhObject *unstructured.Unstructured) error {
	log := logf.FromContext(ctx)
	// "from version" as oldRelease, if return "0.0.0" meaning running on 2.10- release/dummy CI build
	// if oldRelease is lower than 2.14.0(e.g 2.13.x-a), flip disableModelRegistry to false (even the field did not exist)
	if oldRelease.Version.Minor < 14 {
		log.Info("Upgrade force ModelRegistry to false in " + instanceName + " CR due to old release < 2.14.0")
		disableModelRegistryValue := []byte(`{"spec": {"dashboardConfig": {"disableModelRegistry": false}}}`)
		if err := cli.Patch(ctx, odhObject, client.RawPatch(types.MergePatchType, disableModelRegistryValue)); err != nil {
			return fmt.Errorf("error enable ModelRegistry in CR %s : %w", instanceName, err)
		}
		return nil
	}
	log.Info("Upgrade does not force ModelRegistry to false due to from release >= 2.14.0")
	return nil
}

// workaround for RHOAIENG-15328
// TODO: this can be removed from ODH 2.22.
func removeRBACProxyModelRegistry(ctx context.Context, cli client.Client, componentName string, containerName string, applicationNS string) error {
	log := logf.FromContext(ctx)
	deploymentList := &appsv1.DeploymentList{}
	if err := cli.List(ctx, deploymentList, client.InNamespace(applicationNS), client.HasLabels{labels.ODH.Component(componentName)}); err != nil {
		return fmt.Errorf("error fetching list of deployments: %w", err)
	}

	if len(deploymentList.Items) != 1 { // ModelRegistry operator is not deployed
		return nil
	}
	mrDeployment := deploymentList.Items[0]
	mrContainerList := mrDeployment.Spec.Template.Spec.Containers
	// if only one container in deployment, we are already on newer deployment, no need more action
	if len(mrContainerList) == 1 {
		return nil
	}

	log.Info("Upgrade force ModelRegistry to remove container from deployment")
	for i, container := range mrContainerList {
		if container.Name == containerName {
			removeUnusedKubeRbacProxy := []byte(fmt.Sprintf("[{\"op\": \"remove\", \"path\": \"/spec/template/spec/containers/%d\"}]", i))
			if err := cli.Patch(ctx, &mrDeployment, client.RawPatch(types.JSONPatchType, removeUnusedKubeRbacProxy)); err != nil {
				return fmt.Errorf("error removing ModelRegistry %s container from deployment: %w", containerName, err)
			}
			break
		}
	}
	return nil
}

func GetDeployedRelease(ctx context.Context, cli client.Client) (common.Release, error) {
	dsciInstance, err := cluster.GetDSCI(ctx, cli)
	switch {
	case k8serr.IsNotFound(err):
		break
	case err != nil:
		return common.Release{}, err
	default:
		return dsciInstance.Status.Release, nil
	}

	// no DSCI CR found, try with DSC CR
	dscInstances, err := cluster.GetDSC(ctx, cli)
	switch {
	case k8serr.IsNotFound(err):
		break
	case err != nil:
		return common.Release{}, err
	default:
		return dscInstances.Status.Release, nil
	}

	// could be a clean installation or both CRs are deleted already
	return common.Release{}, nil
}

func cleanupNimIntegration(ctx context.Context, cli client.Client, oldRelease common.Release, applicationNS string) error {
	var errs *multierror.Error
	log := logf.FromContext(ctx)

	if oldRelease.Version.Minor >= 14 && oldRelease.Version.Minor <= 16 {
		type objForDel struct {
			obj        client.Object
			name, desc string
		}

		// the following objects created by TP (14-15) and by the first GA (16)
		deleteObjs := []objForDel{
			{
				obj:  &corev1.ConfigMap{},
				name: "nvidia-nim-images-data",
				desc: "data ConfigMap",
			},
			{
				obj:  &templatev1.Template{},
				name: "nvidia-nim-serving-template",
				desc: "runtime Template",
			},
			{
				obj:  &corev1.Secret{},
				name: "nvidia-nim-image-pull",
				desc: "pull Secret",
			},
		}

		// the following objects created by TP (14-15)
		if oldRelease.Version.Minor < 16 {
			deleteObjs = append(deleteObjs,
				objForDel{
					obj:  &batchv1.CronJob{},
					name: "nvidia-nim-periodic-validator",
					desc: "validator CronJob",
				},
				objForDel{
					obj:  &corev1.ConfigMap{},
					name: "nvidia-nim-validation-result",
					desc: "validation result ConfigMap",
				},
				// the api key is also used by GA (16), but cleanup is only required for TP->GA switch
				objForDel{
					obj:  &corev1.Secret{},
					name: "nvidia-nim-access",
					desc: "API key Secret",
				})
		}

		for _, delObj := range deleteObjs {
			if gErr := cli.Get(ctx, types.NamespacedName{Name: delObj.name, Namespace: applicationNS}, delObj.obj); gErr != nil {
				if !k8serr.IsNotFound(gErr) {
					log.V(1).Error(gErr, fmt.Sprintf("failed to get NIM %s %s", delObj.desc, delObj.name))
					errs = multierror.Append(errs, gErr)
				}
			} else {
				if dErr := cli.Delete(ctx, delObj.obj); dErr != nil {
					log.Error(dErr, fmt.Sprintf("failed to remove NIM %s %s", delObj.desc, delObj.name))
					errs = multierror.Append(errs, dErr)
				} else {
					log.Info(fmt.Sprintf("removed NIM %s successfully", delObj.desc))
				}
			}
		}
	}

	return errs.ErrorOrNil()
}

// When upgrading from version 2.16 to 2.17, the odh-model-controller
// fails to be provisioned due to the immutability of the deployment's
// label selectors. In RHOAI ≤ 2.16, the model controller was deployed
// independently by both kserve and modelmesh, leading to variations
// in label assignments depending on the deployment order. During a
// redeployment or upgrade, this error was ignored, and the model
// controller would eventually be reconciled by the appropriate component.
//
// However, in version 2.17, the model controller is now a defined
// dependency with its own fixed labels and selectors. This change
// causes issues during upgrades, as existing deployments cannot be
// modified accordingly.
//
// This function as to stay as long as there is any long term support
// release based on the old logic.
func cleanupModelControllerLegacyDeployment(ctx context.Context, cli client.Client, applicationNS string) error {
	l := logf.FromContext(ctx)

	d := appsv1.Deployment{}
	d.Name = "odh-model-controller"
	d.Namespace = applicationNS

	err := cli.Get(ctx, client.ObjectKeyFromObject(&d), &d)
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("failure getting %s deployment in namespace %s: %w", d.Name, d.Namespace, err)
	}

	if d.Labels[labels.PlatformPartOf] == componentApi.ModelControllerComponentName {
		return nil
	}

	l.Info("deleting legacy deployment", "name", d.Name, "namespace", d.Namespace)

	err = cli.Delete(ctx, &d, client.PropagationPolicy(metav1.DeletePropagationForeground))
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("failure deleting %s deployment in namespace %s: %w", d.Name, d.Namespace, err)
	}

	l.Info("legacy deployment deleted", "name", d.Name, "namespace", d.Namespace)

	return nil
}

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
func PatchOdhDashboardConfig(ctx context.Context, cli client.Client, prevVersion, currVersion common.Release) error {
	log := logf.FromContext(ctx).WithValues(
		"prevVersion", prevVersion.Version.Version,
		"currVersion", currVersion.Version.Version,
		"action", "migration logic for dashboard",
		"kind", "OdhDashboardConfig",
	)

	if !prevVersion.Version.Version.LT(currVersion.Version.Version) {
		log.Info("Skipping patch as current version is not greater than previous version")
		return nil
	}

	dashboardConfig := resources.GvkToUnstructured(gvk.OdhDashboardConfig)

	if err := cluster.GetSingleton(ctx, cli, dashboardConfig); err != nil {
		if meta.IsNoMatchError(err) {
			log.Info("OdhDashboardConfig CRD is not installed, skipping patch")
			return nil
		}
		if k8serr.IsNotFound(err) {
			log.Info("no odhdashboard instance available, hence skipping patch", "namespace", dashboardConfig.GetNamespace(), "name", dashboardConfig.GetName())
			return nil
		}
		return fmt.Errorf("failed to retrieve odhdashboardconfg instance: %w", err)
	}
	log = log.WithValues(
		"namespace", dashboardConfig.GetNamespace(),
		"name", dashboardConfig.GetName(),
	)
	log.Info("Found CR, applying patch")

	patch := dashboardConfig.DeepCopy()
	updates := map[string][]any{
		"notebookSizes":    NotebookSizesData,
		"modelServerSizes": ModelServerSizeData,
	}

	updated, err := updateSpecFields(patch, updates)
	if err != nil {
		return fmt.Errorf("failed to update odhdashboardconfig spec fields: %w", err)
	}

	if !updated {
		log.Info("No changes needed, skipping patch")
		return nil
	}

	if err := cli.Patch(ctx, patch, client.MergeFrom(dashboardConfig)); err != nil {
		return fmt.Errorf("failed to patch CR %s in namespace %s: %w", dashboardConfig.GetName(), dashboardConfig.GetNamespace(), err)
	}

	log.Info("Patched odhdashboardconfig successfully")

	return nil
}

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
func updateSpecFields(obj *unstructured.Unstructured, updates map[string][]any) (bool, error) {
	updated := false

	for field, newData := range updates {
		existingField, exists, err := unstructured.NestedSlice(obj.Object, "spec", field)
		if err != nil {
			return false, fmt.Errorf("failed to get field '%s': %w", field, err)
		}

		if !exists || len(existingField) == 0 {
			if err := unstructured.SetNestedSlice(obj.Object, newData, "spec", field); err != nil {
				return false, fmt.Errorf("failed to set field '%s': %w", field, err)
			}
			updated = true
		}
	}

	return updated, nil
}
