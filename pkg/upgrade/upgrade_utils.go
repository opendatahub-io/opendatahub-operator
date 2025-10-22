// Package upgrade provides functions of upgrade ODH from v1 to v2 and various v2 versions.
// This file contains utility functions for hardware profile migration.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

// ContainerSize represents a container size configuration from OdhDashboardConfig.
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

func getOdhDashboardConfig(ctx context.Context, cli client.Client, applicationNS string) (*unstructured.Unstructured, bool, error) {
	log := logf.FromContext(ctx)
	odhConfig := &unstructured.Unstructured{}
	odhConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)

	// Try to get the OdhDashboardConfig from cluster first
	err := cli.Get(ctx, client.ObjectKey{Name: odhDashboardConfigName, Namespace: applicationNS}, odhConfig)
	if err == nil {
		log.Info("Found OdhDashboardConfig in cluster")
		return odhConfig, true, nil
	}

	// If not found in cluster, check if it's a "not found" error
	if !k8serr.IsNotFound(err) {
		return nil, false, fmt.Errorf("failed to get OdhDashboardConfig from cluster: %w", err)
	}

	log.Info("OdhDashboardConfig not found in cluster, attempting to load from manifests")

	// Try to load from manifests
	manifestConfig, found, err := loadOdhDashboardConfigFromManifests(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load OdhDashboardConfig from manifests: %w", err)
	}

	if !found {
		log.Info("OdhDashboardConfig not found in cluster or manifests")
		return nil, false, nil
	}

	log.Info("Successfully loaded OdhDashboardConfig from manifests")
	return manifestConfig, true, nil
}

func createHardwareProfileFromContainerSize(ctx context.Context, cli client.Client, size ContainerSize,
	sizeType string, notebooksOnlyToleration []corev1.Toleration, applicationNS string) error {
	hwp := generateHardwareProfileFromContainerSize(ctx, size, sizeType, notebooksOnlyToleration, applicationNS)

	if err := cluster.CreateHardwareProfile(ctx, cli, hwp); err != nil {
		return fmt.Errorf("failed to create HardwareProfile resource '%s' for container size '%s' "+
			"(profileType: %s, namespace: %s): %w", hwp.GetName(), size.Name, sizeType, applicationNS, err)
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

// FindContainerCpuMemoryMinMaxCount finds min/max CPU and memory from container sizes in OdhDashboardConfig.
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
			multiErr = multierror.Append(multiErr, fmt.Errorf("failed to parse resource for containerSize %s: %w", size.Name, err))
			continue
		}

		// track minimum cpu/memory
		if minMemory.IsZero() || ReqMem.Cmp(minMemory) < 0 {
			minMemory = ReqMem
		}
		if minCpu.IsZero() || ReqCpu.Cmp(minCpu) < 0 {
			minCpu = ReqCpu
		}

		// track maximum cpu/memory
		if LimitMemory := LimitMem; !LimitMemory.IsZero() && LimitMemory.Cmp(maxMemory) > 0 {
			maxMemory = LimitMemory
		}
		if LimitCpu := LimitCpu; !LimitCpu.IsZero() && LimitCpu.Cmp(maxCpu) > 0 {
			maxCpu = LimitCpu
		}
	}

	if multiErr != nil {
		return nil, multiErr
	}

	// Apply defaults if no values found
	if minMemory.IsZero() {
		minMemory = resource.MustParse(defaultMinMemory)
	}
	if minCpu.IsZero() {
		minCpu = resource.MustParse(defaultMinCpu)
	}

	// Construct result
	result := map[string]string{
		"minMemory": minMemory.String(),
		"minCpu":    minCpu.String(),
	}
	if !maxMemory.IsZero() {
		result["maxMemory"] = maxMemory.String()
	}
	if !maxCpu.IsZero() {
		result["maxCpu"] = maxCpu.String()
	}

	return result, nil
}

// parseCpuMemoryResourceQuantity parses CPU and memory resources from a ContainerSize.
func parseCpuMemoryResourceQuantity(size ContainerSize) (resource.Quantity, resource.Quantity, resource.Quantity, resource.Quantity, *multierror.Error) {
	var multiErr *multierror.Error

	ReqCpu, err := resource.ParseQuantity(size.Resources.Requests.Cpu)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to parse CPU request: %w", err))
	}

	ReqMem, err := resource.ParseQuantity(size.Resources.Requests.Memory)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to parse Memory request: %w", err))
	}

	LimitCpu, err := resource.ParseQuantity(size.Resources.Limits.Cpu)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to parse CPU limit: %w", err))
	}

	LimitMem, err := resource.ParseQuantity(size.Resources.Limits.Memory)
	if err != nil {
		multiErr = multierror.Append(multiErr, fmt.Errorf("failed to parse Memory limit: %w", err))
	}

	return ReqMem, ReqCpu, LimitMem, LimitCpu, multiErr
}

// getContainerSizes extracts container sizes from OdhDashboardConfig.
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

// getNotebooksOnlyToleration extracts the notebooks-only toleration from OdhDashboardConfig.
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
func createHardwareProfileFromAcceleratorProfile(
	ctx context.Context,
	cli client.Client,
	ap unstructured.Unstructured,
	profileType string,
	containerCounts map[string]string,
	toleration []corev1.Toleration,
) error {
	apName := ap.GetName()
	hwp, err := generateHardwareProfileFromAcceleratorProfile(ctx, ap, profileType, containerCounts, toleration)
	if err != nil {
		return fmt.Errorf("failed to generate %s HardwareProfile for AcceleratorProfile '%s' (profileType: %s): %w", profileType, apName, profileType, err)
	}

	if err := cluster.CreateHardwareProfile(ctx, cli, hwp); err != nil {
		return fmt.Errorf("failed to create %s HardwareProfile '%s' for AcceleratorProfile '%s' (profileType: %s): %w", profileType, hwp.GetName(), apName, profileType, err)
	}
	return nil
}

// generateHardwareProfileFromAcceleratorProfile creates a HardwareProfile from an AcceleratorProfile.
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

	// Create annotations
	annotations := createHardwareProfileAnnotations(profileType, displayName, description, !enabled)

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
	hwpName := fmt.Sprintf("%s-%s", apName, profileType)
	log.Info("generated HardwareProfile object from AcceleratorProfile", "name", hwpName, "namespace", apNamespace, "ap", apName)

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

// generateHardwareProfileFromContainerSize creates a HardwareProfile from a ContainerSize.
func generateHardwareProfileFromContainerSize(ctx context.Context, size ContainerSize, profileType string,
	notebooksOnlyToleration []corev1.Toleration, namespace string) *infrav1.HardwareProfile {
	log := logf.FromContext(ctx)

	// Create HWP name
	// Convert size name to lowercase and replace spaces with dashes to comply with the hardwareprofile CRD validation
	hwpName := fmt.Sprintf("%s%s-%s", containerSizeHWPPrefix, strings.ReplaceAll(strings.ToLower(size.Name), " ", "-"), profileType)
	// Create annotations
	annotations := createHardwareProfileAnnotations(profileType, size.Name, "", false)

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

	log.Info("generated HardwareProfile object from ContainerSize", "name", hwpName, "namespace", namespace, "size", size.Name)

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

// getFeatureVisibility returns the dashboard feature visibility string for a profile type.
func getFeatureVisibility(profileType string) string {
	if profileType == serving {
		return featureVisibilityModelServing
	}
	return featureVisibilityWorkbench
}

// getNotebooks retrieves all Notebook resources in the given namespace.
func getNotebooks(ctx context.Context, cli client.Client) ([]*unstructured.Unstructured, error) {
	notebookList := &unstructured.UnstructuredList{}
	notebookList.SetGroupVersionKind(gvk.Notebook)

	err := cli.List(ctx, notebookList)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}

	notebooks := make([]*unstructured.Unstructured, len(notebookList.Items))
	for i := range notebookList.Items {
		notebooks[i] = &notebookList.Items[i]
	}
	return notebooks, nil
}

// getInferenceServices retrieves all InferenceService resources in the given namespace.
func getInferenceServices(ctx context.Context, cli client.Client) ([]*unstructured.Unstructured, error) {
	isvcList := &unstructured.UnstructuredList{}
	isvcList.SetGroupVersionKind(gvk.InferenceServices)

	err := cli.List(ctx, isvcList)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}

	isvcs := make([]*unstructured.Unstructured, len(isvcList.Items))
	for i := range isvcList.Items {
		isvcs[i] = &isvcList.Items[i]
	}
	return isvcs, nil
}

// getSRFromISVC retrieves the ServingRuntime for an InferenceService.
func getSRFromISVC(ctx context.Context, cli client.Client, isvc *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	// Extract runtime name from InferenceService
	runtimeName, found, err := unstructured.NestedString(isvc.Object, "spec", "predictor", "model", "runtime")
	if err != nil || !found {
		return nil, errors.New("runtime not found in InferenceService spec")
	}

	// Fetch the ServingRuntime resource
	servingRuntime := &unstructured.Unstructured{}
	servingRuntime.SetGroupVersionKind(gvk.ServingRuntime)
	err = cli.Get(ctx, client.ObjectKey{Name: runtimeName, Namespace: isvc.GetNamespace()}, servingRuntime)
	return servingRuntime, err
}

// getInferenceServiceResources extracts resource requests/limits from InferenceService.
func getInferenceServiceResources(isvc *unstructured.Unstructured) (map[string]interface{}, error) {
	resources, found, err := unstructured.NestedMap(isvc.Object, "spec", "predictor", "model", "resources")
	if err != nil || !found {
		return nil, errors.New("resources not found")
	}
	return resources, nil
}

// findContainerSizeByResources matches resource specs to container size name.
func findContainerSizeByResources(containerSizes []ContainerSize, resources map[string]interface{}) string {
	if resources == nil {
		return ""
	}

	// Extract requests and limits from resources
	requests, reqOk := resources["requests"].(map[string]interface{})
	limits, limOk := resources["limits"].(map[string]interface{})

	if !reqOk || !limOk {
		return ""
	}

	// Match against each container size
	for _, size := range containerSizes {
		if matchesContainerSize(size, requests, limits) {
			return size.Name
		}
	}

	return ""
}

// matchesContainerSize checks if resources match a container size.
func matchesContainerSize(size ContainerSize, requests, limits map[string]interface{}) bool {
	reqCpu, _ := requests["cpu"].(string)
	reqMem, _ := requests["memory"].(string)
	limCpu, _ := limits["cpu"].(string)
	limMem, _ := limits["memory"].(string)

	return reqCpu == size.Resources.Requests.Cpu &&
		reqMem == size.Resources.Requests.Memory &&
		limCpu == size.Resources.Limits.Cpu &&
		limMem == size.Resources.Limits.Memory
}

// containerSizeExists checks if a size name exists in container sizes.
func containerSizeExists(sizes []ContainerSize, name string) bool {
	for _, size := range sizes {
		if size.Name == name {
			return true
		}
	}
	return false
}

// setHardwareProfileAnnotation sets the HWP annotation on an object and updates it.
func setHardwareProfileAnnotation(ctx context.Context, cli client.Client, obj *unstructured.Unstructured, hwpName string, namespace string) error {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[hardwareProfileNameAnnotation] = hwpName

	// If hardwareprofile name starts with the containersize- prefix or is custom-serving, also set the HWP namespace annotation to the application namespace
	if strings.HasPrefix(hwpName, containerSizeHWPPrefix) || (hwpName == "custom-serving") {
		annotations[hardwareProfileNamespaceAnnotation] = namespace
	}
	obj.SetAnnotations(annotations)

	return cli.Update(ctx, obj)
}

// createHardwareProfileAnnotations creates the standard set of annotations for a HardwareProfile.
// This function ensures consistency across all HardwareProfile creation.
//
// Parameters:
//   - profileType: The type of profile (notebooks, serving, or all)
//   - displayName: The display name for the profile
//   - description: The description for the profile
//   - disabled: Whether the profile is disabled
//
// Returns:
//   - map[string]string: A map of annotation keys to values
func createHardwareProfileAnnotations(profileType, displayName, description string, disabled bool) map[string]string {
	return map[string]string{
		hardwareProfileVisibilityAnnotation:   getFeatureVisibility(profileType),
		hardwareProfileModifiedDateAnnotation: time.Now().Format(time.RFC3339),
		hardwareProfileDisplayNameAnnotation:  displayName,
		hardwareProfileDescriptionAnnotation:  description,
		hardwareProfileDisabledAnnotation:     strconv.FormatBool(disabled),
	}
}
