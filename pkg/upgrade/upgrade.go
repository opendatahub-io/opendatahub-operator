// Package upgrade provides functions of upgrade ODH from v1 to v2 and vaiours v2 versions.
// It contains both the logic to upgrade the ODH components and the logic to cleanup the deprecated resources.
package upgrade

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	templatev1 "github.com/openshift/api/template/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
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
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

type ResourceSpec struct {
	Gvk       schema.GroupVersionKind
	Namespace string
	// path to the field, like "metadata", "name"
	Path []string
	// set of values for the field to match object, any one matches
	Values []string
}

const (
	defaultMinMemory                   = "1Mi"
	defaultMinCpu                      = "1"
	odhDashboardConfigPath             = "/dashboard/rhoai/shared/odhdashboardconfig/odhdashboardconfig.yaml"
	serving                            = "serving"
	notebooks                          = "notebooks"
	acceleratorNameAnnotation          = "opendatahub.io/accelerator-name"
	lastSizeSelectionAnnotation        = "notebooks.opendatahub.io/last-size-selection"
	hardwareProfileNameAnnotation      = "opendatahub.io/hardware-profile-name"
	hardwareProfileNamespaceAnnotation = "opendatahub.io/hardware-profile-namespace"
	containerSizeHWPPrefix             = "containersize-"
)

var defaultResourceLimits = map[string]string{
	"maxMemory": "120Gi",
	"minMemory": "8Gi",
	"maxCpu":    "30",
	"minCpu":    "1",
}

// CreateDefaultDSC creates a default instance of DSC.
// Note: When the platform is not Managed, and a DSC instance already exists, the function doesn't re-create/update the resource.
func CreateDefaultDSC(ctx context.Context, cli client.Client) error {
	// Set the default DSC name depending on the platform
	releaseDataScienceCluster := &dscv2.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: "datasciencecluster.opendatahub.io/v2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsc",
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				AIPipelines: componentApi.DSCDataSciencePipelines{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				Ray: componentApi.DSCRay{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				Kueue: componentApi.DSCKueue{
					KueueManagementSpec: componentApi.KueueManagementSpec{ManagementState: operatorv1.Unmanaged},
				},
				TrustyAI: componentApi.DSCTrustyAI{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				ModelRegistry: componentApi.DSCModelRegistry{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				TrainingOperator: componentApi.DSCTrainingOperator{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				FeastOperator: componentApi.DSCFeastOperator{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
				},
				LlamaStackOperator: componentApi.DSCLlamaStackOperator{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
				},
			},
		},
	}
	err := cluster.CreateWithRetry(ctx, cli, releaseDataScienceCluster) // 1 min timeout
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
	defaultDsciSpec := &dsciv2.DSCInitializationSpec{
		Monitoring: serviceApi.DSCIMonitoring{
			ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
			MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
				Namespace: monNamespace,
				Metrics:   &serviceApi.Metrics{},
			},
		},
		TrustedCABundle: &dsciv2.TrustedCABundleSpec{
			ManagementState: "Managed",
		},
	}

	defaultDsci := &dsciv2.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "dscinitialization.opendatahub.io/v2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: *defaultDsciSpec,
	}

	instances := &dsciv2.DSCInitializationList{}
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
		err := cluster.CreateWithRetry(ctx, cli, defaultDsci) // 1 min timeout
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
	dsciList := &dsciv2.DSCInitializationList{}
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
	// cleanup deprecated kueue ValidatingAdmissionPolicyBinding
	multiErr = multierror.Append(multiErr, cleanupDeprecatedKueueVAPB(ctx, cli))

	// HardwareProfile migration as described in RHOAIENG-33158 and RHOAIENG-33159
	// This includes creating HardwareProfile resources and updating annotations on Notebooks and InferenceServices
	if cluster.GetRelease().Version.Major == 3 && oldReleaseVersion.Version.Major == 2 {
		multiErr = multierror.Append(multiErr, MigrateToInfraHardwareProfiles(ctx, cli, d.Spec.ApplicationsNamespace))
	}

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
		if meta.IsNoMatchError(err) {
			log.Info("CRD not found, will not delete", "gvk", res.Gvk.String())
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
				log.Info("Deleted object", "name", item.GetName(), "gvk", res.Gvk.String(), "namespace", res.Namespace)
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
				log.Info("Attempting to delete", "name", item.GetName(), "namespace", namespace)
				err := cli.Delete(ctx, item)
				if err != nil {
					if k8serr.IsNotFound(err) {
						log.Info("Could not find", "name", item.GetName(), "namespace", namespace)
					} else {
						multiErr = multierror.Append(multiErr, err)
					}
				}
				log.Info("Successfully deleted", "name", item.GetName())
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
		log.Info("Upgrade force BiasMetrics to false due to old release < 2.14.0", "instance", instanceName)
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
		log.Info("Upgrade force ModelRegistry to false due to old release < 2.14.0", "instance", instanceName)
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
					log.V(1).Error(gErr, "failed to get NIM", "desc", delObj.desc, "name", delObj.name)
					errs = multierror.Append(errs, gErr)
				}
			} else {
				if dErr := cli.Delete(ctx, delObj.obj); dErr != nil {
					log.Error(dErr, "failed to remove NIM", "desc", delObj.desc, "name", delObj.name)
					errs = multierror.Append(errs, dErr)
				} else {
					log.Info("removed NIM successfully", "desc", delObj.desc)
				}
			}
		}
	}

	return errs.ErrorOrNil()
}

// When upgrading from version 2.16 to 2.17, the odh-model-controller
// fails to be provisioned due to the immutability of the deployment's
// label selectors. In RHOAI ≤ 2.16, the model controller was deployed
// independently by both kserve and modelmesh components, leading to variations
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

// cleanupDeprecatedKueueVAPB removes the deprecated ValidatingAdmissionPolicyBinding
// that was used in previous versions of Kueue but is no longer needed.
// TODO: Remove this cleanup function in a future release when upgrading from versions
// that contained ValidatingAdmissionPolicyBinding resources (< v2.29.0) is no longer supported.
// This cleanup is only needed for upgrade scenarios from versions that included VAP manifests
// in config/kueue-configs/ocp-4.17-addons/ directory.
func cleanupDeprecatedKueueVAPB(ctx context.Context, cli client.Client) error {
	log := logf.FromContext(ctx)

	// Use the proper ValidatingAdmissionPolicyBinding struct instead of unstructured
	vapb := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kueue-validating-admission-policy-binding",
		},
	}

	// Attempt to delete the resource
	err := cli.Delete(ctx, vapb)
	// VAPB is not a CRD but a core type from k8s, we wanna ensure API version is correct
	if client.IgnoreNotFound(err) != nil && !meta.IsNoMatchError(err) {
		return fmt.Errorf("failed to delete deprecated ValidatingAdmissionPolicyBinding: %w", err)
	}

	if err == nil {
		log.Info("Successfully deleted deprecated ValidatingAdmissionPolicyBinding")
	}

	return nil
}

// MigrateToInfraHardwareProfiles orchestrates all HardwareProfile migrations including resource creation and annotation updates.
// This is the parent function that gets OdhDashboardConfig once and calls all child migration functions.
func MigrateToInfraHardwareProfiles(ctx context.Context, cli client.Client, applicationNS string) error {
	var multiErr *multierror.Error
	log := logf.FromContext(ctx)
	// If application namespace is empty, it means dsci is not available or not initialized properly with application namespace.
	// In this case, we skip the HardwareProfile migration.
	if applicationNS == "" {
		log.Info("Application namespace is empty, skipping HardwareProfile migrations")
		return nil
	}

	// Get OdhDashboardConfig once for all migration functions
	odhConfig, found, err := getOdhDashboardConfig(ctx, cli, applicationNS)
	if err != nil {
		return fmt.Errorf("failed to get OdhDashboardConfig: %w", err)
	}
	if !found {
		log.Info("OdhDashboardConfig not found, skipping HardwareProfile migrations")
		return nil
	}

	// 1. Create 2 HardwareProfiles for each AcceleratorProfile (notebooks and serving)
	multiErr = multierror.Append(multiErr, MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, odhConfig))

	// 2. Create 1 HardwareProfile for each container size (notebook and model server sizes)
	multiErr = multierror.Append(multiErr, MigrateContainerSizesToHardwareProfiles(ctx, cli, applicationNS, odhConfig))

	// 3. Attach HardwareProfile annotations to existing Notebooks
	multiErr = multierror.Append(multiErr, AttachHardwareProfileToNotebooks(ctx, cli, applicationNS, odhConfig))

	// 4. Attach HardwareProfile annotations to existing InferenceServices but create custom-serving HWP first.
	multiErr = multierror.Append(multiErr, CreateCustomServingHardwareProfile(ctx, cli, applicationNS))
	multiErr = multierror.Append(multiErr, AttachHardwareProfileToInferenceServices(ctx, cli, applicationNS, odhConfig))

	return multiErr.ErrorOrNil()
}

// MigrateAcceleratorProfilesToHardwareProfiles migrates AcceleratorProfiles to HardwareProfiles
// as described in RHOAIENG-33158. This creates 2 HardwareProfiles for each AP (notebooks and serving).
func MigrateAcceleratorProfilesToHardwareProfiles(ctx context.Context, cli client.Client, odhConfig *unstructured.Unstructured) error {
	log := logf.FromContext(ctx)

	apList, err := getAcceleratorProfiles(ctx, cli)
	if err != nil {
		return fmt.Errorf("failed to get AcceleratorProfile list: %w", err)
	}
	if len(apList) == 0 {
		log.Info("No AcceleratorProfiles found, skipping migration")
		return nil
	}

	// Get notebooks-only toleration if applicable
	notebooksOnlyToleration, err := getNotebooksOnlyToleration(odhConfig)
	if err != nil {
		return fmt.Errorf("failed to get notebooks-only toleration: %w", err)
	}

	// Calculate container resource limits
	notebookContainerCounts, err := FindContainerCpuMemoryMinMaxCount(odhConfig, "notebookSizes")
	if err != nil {
		return fmt.Errorf("failed to calculate notebook container limits: %w", err)
	}
	// default min limits only for serving HardwareProfile, max limits are not set
	servingContainerCounts := map[string]string{
		"minMemory": "1Gi",
		"minCpu":    "1",
	}

	var multiErr *multierror.Error

	// Create 2 HardwareProfiles for each AcceleratorProfile
	for _, ap := range apList {
		// Create notebooks HardwareProfile
		if err := createHardwareProfileFromAcceleratorProfile(ctx, cli, ap, notebooks, notebookContainerCounts, notebooksOnlyToleration); err != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("failed to create notebooks HardwareProfile for AP %s: %w", ap.GetName(), err))
			continue
		}

		// Create serving HardwareProfile
		if err := createHardwareProfileFromAcceleratorProfile(ctx, cli, ap, serving, servingContainerCounts, nil); err != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("failed to create serving HardwareProfile for AP %s: %w", ap.GetName(), err))
			continue
		}
	}

	return multiErr.ErrorOrNil()
}

// MigrateContainerSizesToHardwareProfiles migrates container sizes to HardwareProfiles
// as described in RHOAIENG-33158. This creates 1 HardwareProfile for each container size.
func MigrateContainerSizesToHardwareProfiles(ctx context.Context, cli client.Client, applicationNS string, odhConfig *unstructured.Unstructured) error {
	var multiErr *multierror.Error

	// Get notebooks-only toleration if applicable
	notebooksOnlyToleration, err := getNotebooksOnlyToleration(odhConfig)
	if err != nil {
		return fmt.Errorf("failed to get notebooks-only toleration: %w", err)
	}

	// Create HardwareProfiles for notebook container sizes
	notebookSizes, err := getContainerSizes(odhConfig, "notebookSizes")
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to get notebook sizes: %w", err))
	}
	if err == nil {
		for _, size := range notebookSizes {
			if err := createHardwareProfileFromContainerSize(ctx, cli, size, notebooks, notebooksOnlyToleration, applicationNS); err != nil {
				multiErr = multierror.Append(multiErr, fmt.Errorf("failed to create HardwareProfile for notebook size %s: %w", size.Name, err))
				continue
			}
		}
	}

	// Create HardwareProfiles for model server container sizes
	modelServerSizes, err := getContainerSizes(odhConfig, "modelServerSizes")
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to get model server sizes: %w", err))
	}
	if err == nil {
		for _, size := range modelServerSizes {
			if err := createHardwareProfileFromContainerSize(ctx, cli, size, serving, nil, applicationNS); err != nil {
				multiErr = multierror.Append(multiErr, fmt.Errorf("failed to create HardwareProfile for model server size %s: %w", size.Name, err))
				continue
			}
		}
	}

	return multiErr.ErrorOrNil()
}

// AttachHardwareProfileToNotebooks migrates AcceleratorProfile and container size annotations
// on Notebooks to HardwareProfile annotations as described in RHOAIENG-33158.
func AttachHardwareProfileToNotebooks(ctx context.Context, cli client.Client, applicationNS string, odhConfig *unstructured.Unstructured) error {
	log := logf.FromContext(ctx)
	var multiErr *multierror.Error

	notebooks, err := getNotebooks(ctx, cli)
	if err != nil {
		return fmt.Errorf("failed to get notebooks: %w", err)
	}

	if len(notebooks) == 0 {
		log.Info("No Notebooks found, skipping annotation migration")
		return nil
	}

	// get the size once for all notebooks.
	containerSizes, err := getContainerSizes(odhConfig, "notebookSizes")
	if err != nil {
		return fmt.Errorf("failed to get container sizes: %w", err)
	}

	for _, notebook := range notebooks {
		// Get annotations once for efficiency
		annotations := notebook.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}

		// Skip if already has HardwareProfile annotation
		if annotations[hardwareProfileNameAnnotation] != "" {
			continue
		}

		var hwpName string
		var migrationSource string

		// Check for AcceleratorProfile annotation first (higher priority)
		if apName := annotations[acceleratorNameAnnotation]; apName != "" {
			// Convert to lowercase and replace spaces with dashes to comply with the hardwareprofile CRD validation
			hwpName = fmt.Sprintf("%s-notebooks", strings.ReplaceAll(strings.ToLower(apName), " ", "-"))
			migrationSource = "AcceleratorProfile annotation"
		} else if sizeSelection := annotations[lastSizeSelectionAnnotation]; sizeSelection != "" && containerSizeExists(containerSizes, sizeSelection) {
			// Handle container size annotation migration
			// If size doesn't exist in OdhDashboardConfig, leave annotation as-is (per requirements)
			hwpName = fmt.Sprintf("%s%s-notebooks", containerSizeHWPPrefix, strings.ReplaceAll(strings.ToLower(sizeSelection), " ", "-"))
			migrationSource = "container size annotation"
		}

		// Set HardwareProfile annotation if we found a migration source
		if hwpName != "" {
			if err := setHardwareProfileAnnotation(ctx, cli, notebook, hwpName, applicationNS); err != nil {
				multiErr = multierror.Append(multiErr, fmt.Errorf("failed to set HardwareProfile annotation for notebook %s: %w", notebook.GetName(), err))
				continue
			}
			log.Info("Migrated annotation to HardwareProfile for Notebook", "notebook", notebook.GetName(), "migrationSource", migrationSource, "hardwareProfile", hwpName)
		}
	}

	return multiErr.ErrorOrNil()
}

func CreateCustomServingHardwareProfile(ctx context.Context, cli client.Client, namespace string) error {
	log := logf.FromContext(ctx)
	// Check if custom-serving HardwareProfile CR already exists
	_, customServingError := cluster.GetHardwareProfile(ctx, cli, "custom-serving", namespace)
	if client.IgnoreNotFound(customServingError) != nil {
		return fmt.Errorf("failed to check HardwareProfile CR: custom-serving %w", customServingError)
	}
	if k8serr.IsNotFound(customServingError) {
		// Create custom-serving HardwareProfile programmatically
		hwp := &infrav1.HardwareProfile{
			TypeMeta: metav1.TypeMeta{
				APIVersion: infrav1.GroupVersion.String(),
				Kind:       "HardwareProfile",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "custom-serving",
				Namespace: namespace,
				Annotations: map[string]string{
					"opendatahub.io/dashboard-feature-visibility": `["model-serving"]`,
					"opendatahub.io/modified-date":                time.Now().Format(time.RFC3339),
					"opendatahub.io/display-name":                 "custom-serving",
					"opendatahub.io/description":                  "",
					"opendatahub.io/disabled":                     "false",
					"opendatahub.io/managed":                      "false",
				},
			},
			Spec: infrav1.HardwareProfileSpec{
				Identifiers: []infrav1.HardwareIdentifier{
					{
						Identifier:   "cpu",
						DisplayName:  "cpu",
						ResourceType: "CPU",
						MinCount:     intstr.FromInt(1),
						DefaultCount: intstr.FromInt(1),
					},
					{
						Identifier:   "memory",
						DisplayName:  "memory",
						ResourceType: "Memory",
						MinCount:     intstr.FromString("1Gi"),
						DefaultCount: intstr.FromString("1Gi"),
					},
				},
			},
		}

		if err := cli.Create(ctx, hwp); err != nil {
			if !k8serr.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create custom-serving HardwareProfile v1: %w", err)
			}
		}
		log.Info("Successfully created custom-serving HardwareProfile", "namespace", namespace)
	}
	return nil
}

// AttachHardwareProfileToInferenceServices migrates AcceleratorProfile annotations from ServingRuntimes
// and matches container sizes on InferenceServices to HardwareProfile annotations as described in RHOAIENG-33158.
func AttachHardwareProfileToInferenceServices(ctx context.Context, cli client.Client, applicationNamespace string, odhConfig *unstructured.Unstructured) error {
	log := logf.FromContext(ctx)
	var multiErr *multierror.Error

	inferenceServices, err := getInferenceServices(ctx, cli)
	if err != nil {
		return fmt.Errorf("failed to get InferenceServices: %w", err)
	}

	if len(inferenceServices) == 0 {
		log.Info("No InferenceServices found, skipping annotation migration")
		return nil
	}

	// get the size once for all inference services.
	containerSizes, err := getContainerSizes(odhConfig, "modelServerSizes")
	if err != nil {
		return fmt.Errorf("failed to get model server sizes: %w", err)
	}

	for _, isvc := range inferenceServices {
		// Get annotations once for efficiency
		isvcAnnotations := isvc.GetAnnotations()
		if isvcAnnotations == nil {
			isvcAnnotations = map[string]string{}
		}

		// Skip if already has HardwareProfile annotation
		if isvcAnnotations[hardwareProfileNameAnnotation] != "" {
			continue
		}

		// Check ServingRuntime for AcceleratorProfile annotation and apply to InferenceService
		servingRuntime, err := getSRFromISVC(ctx, cli, isvc)
		if err == nil {
			runtimeAnnotations := servingRuntime.GetAnnotations()
			if runtimeAnnotations == nil {
				runtimeAnnotations = map[string]string{}
			}
			if apName := runtimeAnnotations[acceleratorNameAnnotation]; apName != "" {
				hwpName := fmt.Sprintf("%s-serving", strings.ReplaceAll(strings.ToLower(apName), " ", "-"))
				if err := setHardwareProfileAnnotation(ctx, cli, isvc, hwpName, applicationNamespace); err != nil {
					multiErr = multierror.Append(multiErr, fmt.Errorf("failed to set HardwareProfile annotation for InferenceService %s: %w", isvc.GetName(), err))
					continue
				}
				log.Info("Migrated ServingRuntime AP annotation to HardwareProfile annotation for InferenceService",
					"isvc", isvc.GetName(), "runtime", servingRuntime.GetName(), "hwp", hwpName)
				continue
			}
		}

		// No AP found, try container size matching
		// Default usign HWProfile CR "custom-serving", update only if we find a matching size
		hwpName := "custom-serving"
		var matchedSize string

		resources, err := getInferenceServiceResources(isvc)
		if err == nil {
			// Try to match resources to a container size
			matchedSize = findContainerSizeByResources(containerSizes, resources)
			if matchedSize != "" {
				hwpName = fmt.Sprintf("%s%s-serving", containerSizeHWPPrefix, strings.ReplaceAll(strings.ToLower(matchedSize), " ", "-"))
			}
		}

		if err := setHardwareProfileAnnotation(ctx, cli, isvc, hwpName, applicationNamespace); err != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("failed to set HardwareProfile annotation for InferenceService %s: %w", isvc.GetName(), err))
		} else {
			// Log after successful annotation setting
			if matchedSize != "" {
				log.Info("Set HardwareProfile annotation for InferenceService based on container size match", "isvc", isvc.GetName(), "size", matchedSize, "hardwareProfile", hwpName)
			} else {
				log.Info("Set HardwareProfile annotation for InferenceService with custom-serving HardwareProfile", "isvc", isvc.GetName(), "hardwareProfile", hwpName)
			}
		}
	}

	return multiErr.ErrorOrNil()
}
