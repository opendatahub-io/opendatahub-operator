// Package upgrade provides functions of upgrade ODH from v1 to v2 and vaiours v2 versions.
// It contains both the logic to upgrade the ODH components and the logic to cleanup the deprecated resources.
package upgrade

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				TrainingOperator: componentApi.DSCTrainingOperator{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
				FeastOperator: componentApi.DSCFeastOperator{
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

// TODO: remove function once we have a generic solution across all components.
func CleanupExistingResource(ctx context.Context,
	cli client.Client,
	platform common.Platform,
) error {
	var multiErr *multierror.Error
	d, err := cluster.GetDSCI(ctx, cli)
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// Cleanup of deprecated default RoleBinding resources
	deprecatedDefaultRoleBinding := []string{d.Spec.ApplicationsNamespace}
	multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, d.Spec.ApplicationsNamespace, deprecatedDefaultRoleBinding, &rbacv1.RoleBindingList{}))

	// only apply on RHOAI since ODH has a different way to create this CR by dashboard
	if platform != cluster.OpenDataHub {
		if err := upgradeODCCR(ctx, cli, "odh-dashboard-config", d.Spec.ApplicationsNamespace); err != nil {
			return err
		}
	}

	// cleanup model controller legacy deployment
	multiErr = multierror.Append(multiErr, cleanupModelControllerLegacyDeployment(ctx, cli, d.Spec.ApplicationsNamespace))

	return multiErr.ErrorOrNil()
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

// TODO: remove after RHOAI 2.25
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
	if prevVersion.Version.Version.GE(currVersion.Version.Version) {
		return nil
	}

	log := logf.FromContext(ctx).WithValues(
		"previousVersion", prevVersion.Version.Version,
		"currentVersion", currVersion.Version.Version,
		"kind", "OdhDashboardConfig",
	)

	dashboardConfig := resources.GvkToUnstructured(gvk.OdhDashboardConfig)

	if err := cluster.GetSingleton(ctx, cli, dashboardConfig); err != nil {
		if meta.IsNoMatchError(err) {
			log.Info("OdhDashboardConfig CRD is not installed, skipping patch")
			return nil
		}
		if k8serr.IsNotFound(err) {
			log.Info("no odhdashboard instance available, skipping patch", "namespace", dashboardConfig.GetNamespace(), "name", dashboardConfig.GetName())
			return nil
		}
		return fmt.Errorf("failed to retrieve odhdashboardconfg instance: %w", err)
	}

	log.Info("Patch OdhDashboardConfig")
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
		return nil
	}

	if err := cli.Patch(ctx, patch, client.MergeFrom(dashboardConfig)); err != nil {
		return fmt.Errorf("failed to patch CR %s in namespace %s: %w", dashboardConfig.GetName(), dashboardConfig.GetNamespace(), err)
	}
	return nil
}
