// Package upgrade provides functions of upgrade ODH from v1 to v2 and vaiours v2 versions.
// It contains both the logic to upgrade the ODH components and the logic to cleanup the deprecated resources.
package upgrade

import (
	"context"
	"fmt"
	"reflect"

	"github.com/hashicorp/go-multierror"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	defaultMinMemory                      = "1Mi"
	defaultMinCpu                         = "1"
	odhDashboardConfigPath                = "/dashboard/rhoai/shared/odhdashboardconfig/odhdashboardconfig.yaml"
	odhDashboardConfigName                = "odh-dashboard-config"
	serving                               = "serving"
	notebooks                             = "notebooks"
	customServing                         = "custom-serving"
	acceleratorNameAnnotation             = "opendatahub.io/accelerator-name"
	acceleratorProfileNamespaceAnnotation = "opendatahub.io/accelerator-profile-namespace"
	lastSizeSelectionAnnotation           = "notebooks.opendatahub.io/last-size-selection"
	hardwareProfileNameAnnotation         = "opendatahub.io/hardware-profile-name"
	hardwareProfileNamespaceAnnotation    = "opendatahub.io/hardware-profile-namespace"
	hardwareProfileManagedAnnotation      = "opendatahub.io/managed"
	hardwareProfileVisibilityAnnotation   = "opendatahub.io/dashboard-feature-visibility"
	hardwareProfileModifiedDateAnnotation = "opendatahub.io/modified-date"
	hardwareProfileDisplayNameAnnotation  = "opendatahub.io/display-name"
	hardwareProfileDescriptionAnnotation  = "opendatahub.io/description"
	hardwareProfileDisabledAnnotation     = "opendatahub.io/disabled"
	featureVisibilityModelServing         = `["model-serving"]`
	featureVisibilityWorkbench            = `["workbench"]`
	containerSizeHWPPrefix                = "containersize-"
)

var defaultResourceLimits = map[string]string{
	"maxMemory": "120Gi",
	"minMemory": "8Gi",
	"maxCpu":    "30",
	"minCpu":    "1",
}

// TODO: remove function once we have a generic solution across all components.
func CleanupExistingResource(ctx context.Context,
	cli client.Client,
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

	// Cleanup of deprecated default RoleBinding resources
	deprecatedDefaultRoleBinding := []string{d.Spec.ApplicationsNamespace}
	multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, d.Spec.ApplicationsNamespace, deprecatedDefaultRoleBinding, &rbacv1.RoleBindingList{}))

	// cleanup model controller legacy deployment
	multiErr = multierror.Append(multiErr, cleanupModelControllerLegacyDeployment(ctx, cli, d.Spec.ApplicationsNamespace))
	// cleanup deprecated kueue ValidatingAdmissionPolicyBinding
	multiErr = multierror.Append(multiErr, cleanupDeprecatedKueueVAPB(ctx, cli))

	// HardwareProfile migration includes creating HardwareProfile resources
	// Check if target infrastructure HardwareProfile CRD exists (indicates we should migrate)
	hasInfraHWP, err := cluster.HasCRD(ctx, cli, gvk.HardwareProfile)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to check HardwareProfile CRD: %w", err))
	} else if hasInfraHWP {
		// Check if source AcceleratorProfile CRD exists (indicates we have data to migrate)
		hasAccelProfile, err := cluster.HasCRD(ctx, cli, gvk.DashboardAcceleratorProfile)
		if err != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("failed to check AcceleratorProfile CRD: %w", err))
		} else if hasAccelProfile {
			// Both CRDs exist, run migration (it's idempotent)
			multiErr = multierror.Append(multiErr, MigrateToInfraHardwareProfiles(ctx, cli, d.Spec.ApplicationsNamespace))
		}
	}

	// GatewayConfig ingressMode migration: preserve LoadBalancer mode for existing deployments
	// Check if GatewayConfig CRD exists (indicates feature is available)
	hasGatewayConfig, err := cluster.HasCRD(ctx, cli, gvk.GatewayConfig)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to check GatewayConfig CRD: %w", err))
	} else if hasGatewayConfig {
		multiErr = multierror.Append(multiErr, MigrateGatewayConfigIngressMode(ctx, cli))
	}

	return multiErr.ErrorOrNil()
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

// MigrateToInfraHardwareProfiles performs one-time migration from AcceleratorProfiles to HardwareProfiles.
// This orchestrates all HardwareProfile migrations including resource creation and annotation updates.
//
// IMPORTANT: This migration uses Create-only semantics. Existing HardwareProfiles are never modified.
// This preserves user customizations and prevents data loss on operator restarts.
//
// Behavior:
//   - Missing HardwareProfiles are created from AcceleratorProfiles and container sizes
//   - Existing HardwareProfiles are skipped (AlreadyExists is not an error)
//   - User modifications to HardwareProfiles persist across migration runs
//
// This function is called on every operator startup via CleanupExistingResource.
// The Create-only approach ensures that frequent operator restarts do not overwrite user changes.
func MigrateToInfraHardwareProfiles(ctx context.Context, cli client.Client, applicationNS string) error {
	var multiErr *multierror.Error
	log := logf.FromContext(ctx)
	// If application namespace is empty, it means dsci is not available or not initialized properly with application namespace.
	// In this case, we skip the HardwareProfile migration.
	if applicationNS == "" {
		log.Info("Application namespace is empty, skipping HardwareProfile migrations")
		return nil
	}

	// Get OdhDashboardConfig to extract container sizes
	odhConfig, found, err := GetOdhDashboardConfig(ctx, cli, applicationNS)
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

	// 3. Create custom-serving HardwareProfile
	multiErr = multierror.Append(multiErr, createCustomServingHardwareProfile(ctx, cli, applicationNS))

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

func createCustomServingHardwareProfile(ctx context.Context, cli client.Client, namespace string) error {
	log := logf.FromContext(ctx)
	// Check if custom-serving HardwareProfile CR already exists
	_, customServingError := cluster.GetHardwareProfile(ctx, cli, customServing, namespace)
	if client.IgnoreNotFound(customServingError) != nil {
		return fmt.Errorf("failed to check HardwareProfile CR: %s %w", customServing, customServingError)
	}
	if k8serr.IsNotFound(customServingError) {
		// Create custom-serving HardwareProfile programmatically
		annotations := createHardwareProfileAnnotations(serving, customServing, "", false)
		annotations[hardwareProfileManagedAnnotation] = "false"

		hwp := &infrav1.HardwareProfile{
			TypeMeta: metav1.TypeMeta{
				APIVersion: infrav1.GroupVersion.String(),
				Kind:       "HardwareProfile",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        customServing,
				Namespace:   namespace,
				Annotations: annotations,
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

		if err := cluster.CreateHardwareProfile(ctx, cli, hwp); err != nil {
			return err
		}
		log.Info("Successfully created HardwareProfile", "name", customServing, "namespace", namespace)
	}
	return nil
}

// MigrateGatewayConfigIngressMode preserves LoadBalancer mode for existing Gateway deployments.
func MigrateGatewayConfigIngressMode(ctx context.Context, cli client.Client) error {
	l := logf.FromContext(ctx)

	gatewayConfig := &unstructured.Unstructured{}
	gatewayConfig.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "services.platform.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "GatewayConfig",
	})

	err := cli.Get(ctx, client.ObjectKey{Name: "default-gateway"}, gatewayConfig)
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("failed to get GatewayConfig: %w", err)
	}

	ingressMode, _, _ := unstructured.NestedString(gatewayConfig.Object, "spec", "ingressMode")
	if ingressMode != "" {
		return nil
	}

	gatewayService := &corev1.Service{}
	err = cli.Get(ctx, client.ObjectKey{
		Name:      gateway.GatewayServiceFullName,
		Namespace: gateway.GatewayNamespace,
	}, gatewayService)
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("failed to get Gateway service: %w", err)
	}

	if gatewayService.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return nil
	}

	l.Info("preserving LoadBalancer ingressMode for existing Gateway")

	patch := client.MergeFrom(gatewayConfig.DeepCopy())
	if err := unstructured.SetNestedField(gatewayConfig.Object, "LoadBalancer", "spec", "ingressMode"); err != nil {
		return fmt.Errorf("failed to set ingressMode field: %w", err)
	}
	if err := cli.Patch(ctx, gatewayConfig, patch); err != nil {
		return fmt.Errorf("failed to patch GatewayConfig: %w", err)
	}

	l.Info("GatewayConfig migrated to ingressMode=LoadBalancer")

	return nil
}
