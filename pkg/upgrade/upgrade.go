// Package upgrade provides functions of upgrade ODH from v1 to v2 and vaiours v2 versions.
// It contains both the logic to upgrade the ODH components and the logic to cleanup the deprecated resources.
package upgrade

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
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

	// Cleanup of deprecated default RoleBinding resources
	deprecatedDefaultRoleBinding := []string{d.Spec.ApplicationsNamespace}
	multiErr = multierror.Append(multiErr, deleteDeprecatedResources(ctx, cli, d.Spec.ApplicationsNamespace, deprecatedDefaultRoleBinding, &rbacv1.RoleBindingList{}))

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

	// Get OdhDashboardConfig to extract container sizes
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
	multiErr = multierror.Append(multiErr, createCustomServingHardwareProfile(ctx, cli, applicationNS))
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
		hwpName := customServing
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
				log.Info("Set HardwareProfile annotation for InferenceService with "+customServing+" HardwareProfile", "isvc", isvc.GetName(), "hardwareProfile", hwpName)
			}
		}
	}

	return multiErr.ErrorOrNil()
}
