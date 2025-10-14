// Package upgrade provides functions of upgrade ODH from v1 to v2 and vaiours v2 versions.
// It contains both the logic to upgrade the ODH components and the logic to cleanup the deprecated resources.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
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
	defaultMinMemory       = "1Mi"
	defaultMinCpu          = "1"
	odhDashboardConfigPath = "/dashboard/rhoai/shared/odhdashboardconfig/odhdashboardconfig.yaml"
	serving                = "serving"
	notebooks              = "notebooks"
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
					KueueManagementSpec: componentApi.KueueManagementSpec{ManagementState: operatorv1.Managed},
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
		ServiceMesh: &infrav1.ServiceMeshSpec{
			ManagementState: "Managed",
			ControlPlane: infrav1.ControlPlaneSpec{
				Name:              "data-science-smcp",
				Namespace:         "istio-system",
				MetricsCollection: "Istio",
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

	// HardwareProfile migration as described in RHOAIENG-33158
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
		if errors.Is(err, &meta.NoKindMatchError{}) {
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

	if oldRelease.Version.Minor >= 14 && oldRelease.Version.Minor <= 16 {
		log := logf.FromContext(ctx)
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

func MigrateToInfraHardwareProfiles(ctx context.Context, cli client.Client, applicationNS string) error {
	var multiErr *multierror.Error
	log := logf.FromContext(ctx)

	// If application namespace is empty, it means dsci is not available or not initialized properly with application namespace.
	// In this case, we skip the HardwareProfile migration.
	if applicationNS == "" {
		log.Info("Application namespace is empty, skipping HardwareProfile migration")
		return nil
	}

	// Get OdhDashboardConfig to extract container sizes
	odhConfig, err := GetOdhDashboardConfig(ctx, cli, applicationNS)
	if err != nil {
		// Check if the error indicates that OdhDashboardConfig was not found anywhere
		if strings.Contains(err.Error(), "not found in cluster or manifests") {
			log.Info("OdhDashboardConfig not found, skipping HardwareProfile migration")
			return nil
		}
		return fmt.Errorf("failed to get OdhDashboardConfig: %w", err)
	}

	// 1. Create 2 HWPs for each AcceleratorProfile (notebooks and serving)
	multiErr = multierror.Append(multiErr, MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, applicationNS, odhConfig))

	// 2. Create 1 HWP for each container size (notebook and model server sizes)
	multiErr = multierror.Append(multiErr, MigrateContainerSizesToHardwareProfiles(ctx, cli, applicationNS, odhConfig))

	return multiErr.ErrorOrNil()
}

// MigrateAcceleratorProfilesToHardwareProfiles migrates AcceleratorProfiles to HardwareProfiles
// as described in RHOAIENG-33158. This creates 2 HWPs for each AP (notebooks and serving).
func MigrateAcceleratorProfilesToHardwareProfiles(ctx context.Context, cli client.Client, applicationNS string, odhConfig *unstructured.Unstructured) error {
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
	// default min limits only for serving HWP, max limits are not set
	servingContainerCounts := map[string]string{
		"minMemory": "1Gi",
		"minCpu":    "1",
	}

	var multiErr *multierror.Error

	// Create 2 HWPs for each AcceleratorProfile
	for _, ap := range apList {
		// Create notebooks HWP
		if err := createHardwareProfileFromAcceleratorProfile(ctx, cli, ap, notebooks, notebookContainerCounts, notebooksOnlyToleration); err != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("failed to create notebooks HWP for AP %s: %w", ap.GetName(), err))
			continue
		}

		// Create serving HWP
		if err := createHardwareProfileFromAcceleratorProfile(ctx, cli, ap, serving, servingContainerCounts, nil); err != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("failed to create serving HWP for AP %s: %w", ap.GetName(), err))
			continue
		}
	}

	return multiErr.ErrorOrNil()
}

func createHardwareProfileFromAcceleratorProfile(ctx context.Context, cli client.Client,
	ap unstructured.Unstructured, profileType string, containerCounts map[string]string,
	toleration []corev1.Toleration) error {
	apName := ap.GetName()
	hwp, err := generateHardwareProfileFromAcceleratorProfile(ctx, ap, profileType, containerCounts, toleration)
	if err != nil {
		return fmt.Errorf("failed to generate %s HardwareProfile for AcceleratorProfile '%s' (profileType: %s): %w", profileType, apName, profileType, err)
	}

	if err := cli.Create(ctx, hwp); err != nil {
		if !k8serr.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create %s HardwareProfile '%s' for AcceleratorProfile '%s' (profileType: %s): %w", profileType, hwp.GetName(), apName, profileType, err)
		}
	}
	return nil
}

func getAcceleratorProfiles(ctx context.Context, cli client.Client) ([]unstructured.Unstructured, error) {
	apList := &unstructured.UnstructuredList{}
	apList.SetGroupVersionKind(gvk.DashboardAcceleratorProfile)
	err := cli.List(ctx, apList)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get AcceleratorProfile list: %w", err)
	}
	return apList.Items, nil
}

// MigrateContainerSizesToHardwareProfiles migrates container sizes to HardwareProfiles
// as described in RHOAIENG-33158. This creates 1 HWP for each container size.
func MigrateContainerSizesToHardwareProfiles(ctx context.Context, cli client.Client, applicationNS string, odhConfig *unstructured.Unstructured) error {
	var multiErr *multierror.Error

	// Get notebooks-only toleration if applicable
	notebooksOnlyToleration, err := getNotebooksOnlyToleration(odhConfig)
	if err != nil {
		return fmt.Errorf("failed to get notebooks-only toleration: %w", err)
	}

	// Create HWPs for notebook container sizes
	notebookSizes, err := getContainerSizes(odhConfig, "notebookSizes")
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to get notebook sizes: %w", err))
	} else {
		for _, size := range notebookSizes {
			if err := createHardwareProfileFromContainerSize(ctx, cli, size, notebooks, notebooksOnlyToleration, applicationNS); err != nil {
				multiErr = multierror.Append(multiErr, fmt.Errorf("failed to create HWP for notebook size %s: %w", size.Name, err))
				continue
			}
		}
	}

	// Create HWPs for model server container sizes
	modelServerSizes, err := getContainerSizes(odhConfig, "modelServerSizes")
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to get model server sizes: %w", err))
	} else {
		for _, size := range modelServerSizes {
			if err := createHardwareProfileFromContainerSize(ctx, cli, size, serving, nil, applicationNS); err != nil {
				multiErr = multierror.Append(multiErr, fmt.Errorf("failed to create HWP for model server size %s: %w", size.Name, err))
				continue
			}
		}
	}

	return multiErr.ErrorOrNil()
}

func createHardwareProfileFromContainerSize(ctx context.Context, cli client.Client, size ContainerSize,
	sizeType string, notebooksOnlyToleration []corev1.Toleration, applicationNS string) error {
	hwp := generateHardwareProfileFromContainerSize(ctx, size, sizeType, notebooksOnlyToleration, applicationNS)

	if err := cli.Create(ctx, hwp); err != nil {
		if !k8serr.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create HardwareProfile resource '%s' for container size '%s' "+
				"(profileType: %s, namespace: %s): %w", hwp.GetName(), size.Name, sizeType, applicationNS, err)
		}
	}
	return nil
}

// loadOdhDashboardConfigFromManifests attempts to load OdhDashboardConfig from manifest files.
// It searches for manifest files in the expected locations and returns the first valid OdhDashboardConfig found.
func loadOdhDashboardConfigFromManifests(ctx context.Context) (*unstructured.Unstructured, bool, error) {
	log := logf.FromContext(ctx)

	manifestPath := deploy.DefaultManifestPath + odhDashboardConfigPath
	_, err := os.Stat(manifestPath)
	if err == nil {
		log.Info("Found OdhDashboardConfig manifest", "path", manifestPath)

		// Read the manifest file
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			log.Error(err, "Failed to read manifest file", "path", manifestPath)
			return nil, false, err
		}

		// Parse the YAML content
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(content, &obj); err != nil {
			log.Error(err, "Failed to parse manifest YAML", "path", manifestPath)
			return nil, false, err
		}

		// Verify it's an OdhDashboardConfig
		if obj.GetKind() == "OdhDashboardConfig" {
			log.Info("Successfully loaded OdhDashboardConfig from manifest", "path", manifestPath)
			return &obj, true, nil
		}
	}
	return nil, false, err
}

func GetOdhDashboardConfig(ctx context.Context, cli client.Client, applicationNS string) (*unstructured.Unstructured, error) {
	log := logf.FromContext(ctx)
	odhConfig := &unstructured.Unstructured{}
	odhConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)

	// Try to get the OdhDashboardConfig from cluster first
	err := cli.Get(ctx, client.ObjectKey{Name: "odh-dashboard-config", Namespace: applicationNS}, odhConfig)
	if err == nil {
		log.Info("Found OdhDashboardConfig in cluster")
		return odhConfig, nil
	}

	// If not found in cluster, check if it's a "not found" error
	if !k8serr.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get OdhDashboardConfig from cluster: %w", err)
	}

	log.Info("OdhDashboardConfig not found in cluster, attempting to load from manifests")

	// Try to load from manifests
	manifestConfig, found, err := loadOdhDashboardConfigFromManifests(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load OdhDashboardConfig from manifests: %w", err)
	}

	if !found {
		return nil, errors.New("OdhDashboardConfig not found in cluster or manifests - skipping migration")
	}

	log.Info("Successfully loaded OdhDashboardConfig from manifests")
	return manifestConfig, nil
}

func FindContainerCpuMemoryMinMaxCount(odhConfig *unstructured.Unstructured, sizeType string) (map[string]string, error) {
	containerSizes, err := getContainerSizes(odhConfig, sizeType)
	if err != nil {
		return nil, fmt.Errorf("failed to get container sizes for %s: %w", sizeType, err)
	}

	if len(containerSizes) == 0 {
		return defaultResourceLimits, nil
	}

	limits, err := FindCpuMemoryMinMaxCountFromContainerSizes(containerSizes)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate resource limits from container sizes: %w", err)
	}

	return limits, nil
}

// FindCpuMemoryMinMaxCountFromContainerSizes finds minimum and maximum cpu, memory counts available across all container sizes.
func FindCpuMemoryMinMaxCountFromContainerSizes(containerSizes []ContainerSize) (map[string]string, error) {
	var maxMemory, minMemory, maxCpu, minCpu resource.Quantity

	var multiErr *multierror.Error

	for _, size := range containerSizes {
		ReqMem, ReqCpu, LimitMem, LimitCpu, err := parseCpuMemoryResourceQuantity(size)
		if err != nil {
			multiErr = multierror.Append(multiErr, err)
			continue
		}
		// minMemory is the smallest request memory across all container sizes
		if minMemory.IsZero() || minMemory.Cmp(ReqMem) > 0 {
			minMemory = ReqMem
		}
		// minCpu is the smallest request cpu across all container sizes
		if minCpu.IsZero() || minCpu.Cmp(ReqCpu) > 0 {
			minCpu = ReqCpu
		}
		// maxMemory is the largest limit memory
		if maxMemory.IsZero() || maxMemory.Cmp(LimitMem) < 0 {
			maxMemory = LimitMem
		}
		// maxCpu is the largest limit cpu
		if maxCpu.IsZero() || maxCpu.Cmp(LimitCpu) < 0 {
			maxCpu = LimitCpu
		}
	}

	if multiErr.ErrorOrNil() != nil {
		return nil, multiErr.ErrorOrNil()
	}

	// Apply defaults if no values found
	result := make(map[string]string)

	if minMemory.IsZero() {
		minMemory = resource.MustParse(defaultMinMemory)
	}
	if minCpu.IsZero() {
		minCpu = resource.MustParse(defaultMinCpu)
	}

	result["minMemory"] = minMemory.String()
	result["minCpu"] = minCpu.String()

	if !maxMemory.IsZero() {
		result["maxMemory"] = maxMemory.String()
	}
	if !maxCpu.IsZero() {
		result["maxCpu"] = maxCpu.String()
	}

	return result, nil
}

func parseCpuMemoryResourceQuantity(size ContainerSize) (resource.Quantity, resource.Quantity, resource.Quantity, resource.Quantity, *multierror.Error) {
	var multiErr *multierror.Error

	ReqMem, err := resource.ParseQuantity(size.Resources.Requests.Memory)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to parse request memory for size %s: %w", size.Name, err))
	}
	ReqCpu, err := resource.ParseQuantity(size.Resources.Requests.Cpu)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to parse request cpu for size %s: %w", size.Name, err))
	}

	LimitMem, err := resource.ParseQuantity(size.Resources.Limits.Memory)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to parse limit memory for size %s: %w", size.Name, err))
	}
	LimitCpu, err := resource.ParseQuantity(size.Resources.Limits.Cpu)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to parse limit cpu for size %s: %w", size.Name, err))
	}

	return ReqMem, ReqCpu, LimitMem, LimitCpu, multiErr
}

type ContainerSize struct {
	Name      string
	Resources struct {
		Requests struct {
			Cpu    string
			Memory string
		}
		Limits struct {
			Cpu    string
			Memory string
		}
	}
}

func getContainerSizes(odhConfig *unstructured.Unstructured, sizeType string) ([]ContainerSize, error) {
	spec, found, err := unstructured.NestedMap(odhConfig.Object, "spec")
	if err != nil || !found {
		return nil, errors.New("failed to get spec from OdhDashboardConfig")
	}

	sizes, found, err := unstructured.NestedSlice(spec, sizeType)
	if err != nil || !found {
		return []ContainerSize{}, err
	}

	containerSizes := make([]ContainerSize, 0, len(sizes))
	for _, size := range sizes {
		sizeMap, ok := size.(map[string]interface{})
		if !ok {
			continue
		}

		containerSize := ContainerSize{}
		if name, ok := sizeMap["name"].(string); ok {
			containerSize.Name = name
		}

		if resources, ok := sizeMap["resources"].(map[string]interface{}); ok {
			if requests, ok := resources["requests"].(map[string]interface{}); ok {
				if cpu, ok := requests["cpu"].(string); ok {
					containerSize.Resources.Requests.Cpu = cpu
				}
				if memory, ok := requests["memory"].(string); ok {
					containerSize.Resources.Requests.Memory = memory
				}
			}
			if limits, ok := resources["limits"].(map[string]interface{}); ok {
				if cpu, ok := limits["cpu"].(string); ok {
					containerSize.Resources.Limits.Cpu = cpu
				}
				if memory, ok := limits["memory"].(string); ok {
					containerSize.Resources.Limits.Memory = memory
				}
			}
		}

		containerSizes = append(containerSizes, containerSize)
	}

	return containerSizes, nil
}

func getNotebooksOnlyToleration(odhConfig *unstructured.Unstructured) ([]corev1.Toleration, error) {
	spec, found, err := unstructured.NestedMap(odhConfig.Object, "spec")
	if err != nil || !found {
		return nil, err
	}

	notebookController, found, err := unstructured.NestedMap(spec, "notebookController")
	if err != nil || !found {
		return nil, err
	}

	enabled, found, err := unstructured.NestedBool(notebookController, "enabled")
	if err != nil || !found || !enabled {
		return nil, err
	}

	tolerationSettings, found, err := unstructured.NestedMap(notebookController, "notebookTolerationSettings")
	if err != nil || !found {
		return nil, err
	}

	tolerationEnabled, found, err := unstructured.NestedBool(tolerationSettings, "enabled")
	if err != nil || !found || !tolerationEnabled {
		return nil, err
	}

	key, found, err := unstructured.NestedString(tolerationSettings, "key")
	if err != nil || !found || key == "" {
		return nil, err
	}

	// Create toleration from settings
	toleration := corev1.Toleration{
		Key: key,
	}

	if value, found, err := unstructured.NestedString(tolerationSettings, "value"); err == nil && found {
		toleration.Value = value
	}

	if operator, found, err := unstructured.NestedString(tolerationSettings, "operator"); err == nil && found {
		toleration.Operator = corev1.TolerationOperator(operator)
	}

	if effect, found, err := unstructured.NestedString(tolerationSettings, "effect"); err == nil && found {
		toleration.Effect = corev1.TaintEffect(effect)
	}

	return []corev1.Toleration{toleration}, nil
}

func generateHardwareProfileFromAcceleratorProfile(ctx context.Context, ap unstructured.Unstructured, profileType string,
	containerCounts map[string]string, notebooksOnlyToleration []corev1.Toleration) (*infrav1.HardwareProfile, error) {
	log := logf.FromContext(ctx)

	// Extract AP fields
	apName := ap.GetName()
	apNamespace := ap.GetNamespace()

	spec, found, err := unstructured.NestedMap(ap.Object, "spec")
	if err != nil || !found {
		return nil, errors.New("failed to get spec from AcceleratorProfile")
	}

	identifier, _ := spec["identifier"].(string)
	displayName, _ := spec["displayName"].(string)
	description, _ := spec["description"].(string)
	enabled, _ := spec["enabled"].(bool)

	// Create HWP name
	hwpName := fmt.Sprintf("%s-%s", apName, profileType)

	// Create annotations
	annotations := map[string]string{
		"opendatahub.io/dashboard-feature-visibility": GetFeatureVisibility(profileType),
		"opendatahub.io/modified-date":                time.Now().Format(time.RFC3339),
		"opendatahub.io/display-name":                 displayName,
		"opendatahub.io/description":                  description,
		"opendatahub.io/disabled":                     strconv.FormatBool(!enabled),
	}

	// Copy existing annotations from AP
	if apAnnotations := ap.GetAnnotations(); apAnnotations != nil {
		for k, v := range apAnnotations {
			annotations[k] = v
		}
	}

	// Create identifiers
	identifiers := []infrav1.HardwareIdentifier{
		{
			Identifier:   identifier,
			DisplayName:  identifier,
			ResourceType: "Accelerator",
			MinCount:     intstr.FromInt(1),
			DefaultCount: intstr.FromInt(1),
		},
		{
			Identifier:   "cpu",
			DisplayName:  "cpu",
			ResourceType: "CPU",
			MinCount:     intstr.FromString(containerCounts["minCpu"]),
			DefaultCount: intstr.FromString(containerCounts["minCpu"]),
		},
		{
			Identifier:   "memory",
			DisplayName:  "memory",
			ResourceType: "Memory",
			MinCount:     intstr.FromString(containerCounts["minMemory"]),
			DefaultCount: intstr.FromString(containerCounts["minMemory"]),
		},
	}

	// Add max counts for notebooks profile
	if profileType == notebooks {
		if maxCpu, ok := containerCounts["maxCpu"]; ok && maxCpu != "" {
			identifiers[1].MaxCount = &intstr.IntOrString{Type: intstr.String, StrVal: maxCpu}
		}
		if maxMemory, ok := containerCounts["maxMemory"]; ok && maxMemory != "" {
			identifiers[2].MaxCount = &intstr.IntOrString{Type: intstr.String, StrVal: maxMemory}
		}
	}

	// Get tolerations from AP
	var tolerations []corev1.Toleration
	if apTolerations, found, err := unstructured.NestedSlice(spec, "tolerations"); err == nil && found {
		for _, tol := range apTolerations {
			if tolMap, ok := tol.(map[string]interface{}); ok {
				toleration := corev1.Toleration{}
				if key, ok := tolMap["key"].(string); ok {
					toleration.Key = key
				}
				if value, ok := tolMap["value"].(string); ok {
					toleration.Value = value
				}
				if operator, ok := tolMap["operator"].(string); ok {
					toleration.Operator = corev1.TolerationOperator(operator)
				}
				if effect, ok := tolMap["effect"].(string); ok {
					toleration.Effect = corev1.TaintEffect(effect)
				}
				tolerations = append(tolerations, toleration)
			}
		}
	}

	// Add notebooks-only toleration for notebooks profile
	if profileType == notebooks && len(notebooksOnlyToleration) > 0 {
		tolerations = append(tolerations, notebooksOnlyToleration...)
	}

	// Create scheduling spec if tolerations exist
	var schedulingSpec *infrav1.SchedulingSpec
	if len(tolerations) > 0 {
		schedulingSpec = &infrav1.SchedulingSpec{
			SchedulingType: infrav1.NodeScheduling,
			Node: &infrav1.NodeSchedulingSpec{
				Tolerations: tolerations,
			},
		}
	}

	log.Info("successfully generated HardwareProfile from AcceleratorProfile", "name", hwpName, "namespace", apNamespace, "ap", apName)

	return &infrav1.HardwareProfile{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrav1.GroupVersion.String(),
			Kind:       "HardwareProfile",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        hwpName,
			Namespace:   apNamespace,
			Annotations: annotations,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers:    identifiers,
			SchedulingSpec: schedulingSpec,
		},
	}, nil
}

func generateHardwareProfileFromContainerSize(ctx context.Context, size ContainerSize, profileType string,
	notebooksOnlyToleration []corev1.Toleration, namespace string) *infrav1.HardwareProfile {
	log := logf.FromContext(ctx)

	// Create HWP name
	hwpName := fmt.Sprintf("containerSize-%s-%s", size.Name, profileType)
	// Convert to lowercase and replace spaces with dashes to comply with the hardwareprofile CRD validation
	hwpName = strings.ReplaceAll(strings.ToLower(hwpName), " ", "-")
	// Create annotations
	annotations := map[string]string{
		"opendatahub.io/dashboard-feature-visibility": GetFeatureVisibility(profileType),
		"opendatahub.io/modified-date":                time.Now().Format(time.RFC3339),
		"opendatahub.io/display-name":                 size.Name,
		"opendatahub.io/description":                  "",
		"opendatahub.io/disabled":                     "false",
	}

	// Create identifiers
	identifiers := []infrav1.HardwareIdentifier{
		{
			Identifier:   "cpu",
			DisplayName:  "cpu",
			ResourceType: "CPU",
			MinCount:     intstr.FromString(size.Resources.Requests.Cpu),
			MaxCount:     &intstr.IntOrString{Type: intstr.String, StrVal: size.Resources.Limits.Cpu},
			DefaultCount: intstr.FromString(size.Resources.Requests.Cpu),
		},
		{
			Identifier:   "memory",
			DisplayName:  "memory",
			ResourceType: "Memory",
			MinCount:     intstr.FromString(size.Resources.Requests.Memory),
			MaxCount:     &intstr.IntOrString{Type: intstr.String, StrVal: size.Resources.Limits.Memory},
			DefaultCount: intstr.FromString(size.Resources.Requests.Memory),
		},
	}

	// Create scheduling spec if tolerations exist
	var schedulingSpec *infrav1.SchedulingSpec
	if len(notebooksOnlyToleration) > 0 {
		schedulingSpec = &infrav1.SchedulingSpec{
			SchedulingType: infrav1.NodeScheduling,
			Node: &infrav1.NodeSchedulingSpec{
				Tolerations: notebooksOnlyToleration,
			},
		}
	}

	log.Info("successfully generated HardwareProfile from ContainerSize", "name", hwpName, "namespace", namespace, "size", size.Name)

	return &infrav1.HardwareProfile{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrav1.GroupVersion.String(),
			Kind:       "HardwareProfile",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        hwpName,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers:    identifiers,
			SchedulingSpec: schedulingSpec,
		},
	}
}

func GetFeatureVisibility(profileType string) string {
	switch profileType {
	case notebooks:
		return "workbench"
	case serving:
		return "model-serving"
	default:
		return "workbench"
	}
}
