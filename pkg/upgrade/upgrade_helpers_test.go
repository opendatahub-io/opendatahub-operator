package upgrade_test

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	hardwareProfileKind     = "HardwareProfile"
	notebooksProfileType    = "notebooks"
	gatewayServiceName      = "data-science-gateway-data-science-gateway-class"
	gatewayServiceNamespace = "openshift-ingress"

	// Hardware profile identifier counts.
	expectedAcceleratorProfileIdentifierCount = 3 // Accelerator + CPU + Memory
	expectedContainerSizeIdentifierCount      = 2 // CPU + Memory
)

// Helper functions for creating test objects.

func createTestOdhDashboardConfig(t *testing.T, namespace string) *unstructured.Unstructured {
	t.Helper()
	odhConfig := &unstructured.Unstructured{}
	odhConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhConfig.SetName("odh-dashboard-config")
	odhConfig.SetNamespace(namespace)

	spec := map[string]interface{}{}
	spec["notebookSizes"] = []interface{}{
		map[string]interface{}{
			"name": "Small",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"memory": "8Gi",
					"cpu":    "1",
				},
				"limits": map[string]interface{}{
					"memory": "8Gi",
					"cpu":    "2",
				},
			},
		},
		map[string]interface{}{
			"name": "Medium",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"memory": "24Gi",
					"cpu":    "3",
				},
				"limits": map[string]interface{}{
					"memory": "24Gi",
					"cpu":    "6",
				},
			},
		},
		map[string]interface{}{
			"name": "X Large",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"memory": "120Gi",
					"cpu":    "15",
				},
				"limits": map[string]interface{}{
					"memory": "120Gi",
					"cpu":    "30",
				},
			},
		},
		map[string]interface{}{
			"name": "Large",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"memory": "56Gi",
					"cpu":    "7",
				},
				"limits": map[string]interface{}{
					"memory": "56Gi",
					"cpu":    "14",
				},
			},
		},
	}

	spec["modelServerSizes"] = []interface{}{
		map[string]interface{}{
			"name": "Large",
			"resources": map[string]interface{}{
				"limits": map[string]interface{}{
					"cpu":    "10",
					"memory": "20Gi",
				},
				"requests": map[string]interface{}{
					"cpu":    "6",
					"memory": "16Gi",
				},
			},
		},
		map[string]interface{}{
			"name": "Small",
			"resources": map[string]interface{}{
				"limits": map[string]interface{}{
					"cpu":    "2",
					"memory": "8Gi",
				},
				"requests": map[string]interface{}{
					"cpu":    "1",
					"memory": "4Gi",
				},
			},
		},
		map[string]interface{}{
			"name": "Medium",
			"resources": map[string]interface{}{
				"limits": map[string]interface{}{
					"cpu":    "8",
					"memory": "10Gi",
				},
				"requests": map[string]interface{}{
					"cpu":    "4",
					"memory": "8Gi",
				},
			},
		},
	}

	err := unstructured.SetNestedMap(odhConfig.Object, spec, "spec")
	if err != nil {
		t.Fatalf("failed to create test OdhDashboardConfig: %v", err)
	}
	return odhConfig
}

func createTestOdhDashboardConfigWithTolerations(t *testing.T, namespace string) *unstructured.Unstructured {
	t.Helper()
	odhConfig := createTestOdhDashboardConfig(t, namespace)

	// Add notebook controller toleration settings
	notebookController := map[string]interface{}{
		"enabled": true,
		"notebookTolerationSettings": map[string]interface{}{
			"enabled":  true,
			"key":      "notebooks-only",
			"value":    "true",
			"operator": "Equal",
			"effect":   "NoSchedule",
		},
	}

	spec, found, err := unstructured.NestedMap(odhConfig.Object, "spec")
	if err != nil {
		t.Fatalf("failed to get spec from OdhDashboardConfig: %v", err)
	}
	if !found {
		t.Fatal("spec not found in OdhDashboardConfig")
	}

	spec["notebookController"] = notebookController
	err = unstructured.SetNestedMap(odhConfig.Object, spec, "spec")
	if err != nil {
		t.Fatalf("failed to create test OdhDashboardConfig with tolerations: %v", err)
	}

	return odhConfig
}

func createTestOdhDashboardConfigWithoutSizes(t *testing.T, namespace string) *unstructured.Unstructured {
	t.Helper()
	odhConfig := &unstructured.Unstructured{}
	odhConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhConfig.SetName("odh-dashboard-config")
	odhConfig.SetNamespace(namespace)

	// Set empty spec
	spec := map[string]interface{}{}
	err := unstructured.SetNestedMap(odhConfig.Object, spec, "spec")
	if err != nil {
		t.Fatalf("failed to create test OdhDashboardConfig without sizes: %v", err)
	}
	return odhConfig
}

func createTestOdhDashboardConfigWithMalformedSizes(t *testing.T, namespace string) *unstructured.Unstructured {
	t.Helper()
	odhConfig := &unstructured.Unstructured{}
	odhConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhConfig.SetName("odh-dashboard-config")
	odhConfig.SetNamespace(namespace)

	// Set spec with malformed container sizes
	spec := map[string]interface{}{
		"notebookSizes": []interface{}{
			map[string]interface{}{
				"name":      "malformed",
				"resources": "invalid", // Should be a map, not a string
			},
		},
	}

	err := unstructured.SetNestedMap(odhConfig.Object, spec, "spec")
	if err != nil {
		t.Fatalf("failed to create test OdhDashboardConfig with malformed sizes: %v", err)
	}
	return odhConfig
}

func createTestAcceleratorProfile(t *testing.T, namespace string) *unstructured.Unstructured {
	t.Helper()
	ap := &unstructured.Unstructured{}
	ap.SetGroupVersionKind(gvk.DashboardAcceleratorProfile)
	ap.SetName("test-ap")
	ap.SetNamespace(namespace)

	spec := map[string]interface{}{
		"identifier":  "nvidia.com/gpu",
		"displayName": "NVIDIA GPU",
		"description": "NVIDIA GPU accelerator",
		"enabled":     true,
	}

	err := unstructured.SetNestedMap(ap.Object, spec, "spec")
	if err != nil {
		t.Fatalf("failed to create test AcceleratorProfile: %v", err)
	}
	return ap
}

func createTestAcceleratorProfileWithTolerations(t *testing.T, namespace string) *unstructured.Unstructured {
	t.Helper()
	ap := createTestAcceleratorProfile(t, namespace)

	spec, found, err := unstructured.NestedMap(ap.Object, "spec")
	if err != nil {
		t.Fatalf("failed to get spec from AcceleratorProfile: %v", err)
	}
	if !found {
		t.Fatal("spec not found in AcceleratorProfile")
	}

	tolerations := []interface{}{
		map[string]interface{}{
			"key":      "nvidia.com/gpu",
			"operator": "Equal",
			"value":    "true",
			"effect":   "NoSchedule",
		},
	}
	spec["tolerations"] = tolerations
	err = unstructured.SetNestedMap(ap.Object, spec, "spec")
	if err != nil {
		t.Fatalf("failed to create test AcceleratorProfile with tolerations: %v", err)
	}

	return ap
}

func createMalformedAcceleratorProfile(t *testing.T, namespace string) *unstructured.Unstructured {
	t.Helper()
	ap := &unstructured.Unstructured{}
	ap.SetGroupVersionKind(gvk.DashboardAcceleratorProfile)
	ap.SetName("malformed-ap")
	ap.SetNamespace(namespace)

	// Missing spec field
	return ap
}

func createTestNotebook(namespace, name string) *unstructured.Unstructured {
	notebook := &unstructured.Unstructured{}
	notebook.SetGroupVersionKind(gvk.Notebook)
	notebook.SetName(name)
	notebook.SetNamespace(namespace)
	return notebook
}

func createTestServingRuntime(namespace, name string) *unstructured.Unstructured {
	servingRuntime := &unstructured.Unstructured{}
	servingRuntime.SetGroupVersionKind(gvk.ServingRuntime)
	servingRuntime.SetName(name)
	servingRuntime.SetNamespace(namespace)
	return servingRuntime
}

func createTestInferenceService(namespace, name, runtimeName string) *unstructured.Unstructured {
	isvc := &unstructured.Unstructured{}
	isvc.SetGroupVersionKind(gvk.InferenceServices)
	isvc.SetName(name)
	isvc.SetNamespace(namespace)

	if runtimeName != "" {
		spec := map[string]interface{}{
			"predictor": map[string]interface{}{
				"model": map[string]interface{}{
					"runtime": runtimeName,
				},
			},
		}
		isvc.Object["spec"] = spec
	}

	return isvc
}

func createTestInferenceServiceWithResources(namespace, name, reqCpu, reqMem, limCpu, limMem string) *unstructured.Unstructured {
	isvc := &unstructured.Unstructured{}
	isvc.SetGroupVersionKind(gvk.InferenceServices)
	isvc.SetName(name)
	isvc.SetNamespace(namespace)

	spec := map[string]interface{}{
		"predictor": map[string]interface{}{
			"model": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    reqCpu,
						"memory": reqMem,
					},
					"limits": map[string]interface{}{
						"cpu":    limCpu,
						"memory": limMem,
					},
				},
			},
		},
	}
	isvc.Object["spec"] = spec

	return isvc
}

func createTestGatewayService(serviceType corev1.ServiceType) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayServiceName,
			Namespace: gatewayServiceNamespace,
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
		},
	}
}

func createTestGatewayConfig() *unstructured.Unstructured {
	gatewayConfig := &unstructured.Unstructured{}
	gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
	gatewayConfig.SetName("default-gateway")
	spec := map[string]interface{}{}
	// Intentionally not handling error - empty spec is valid for tests
	_ = unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
	return gatewayConfig
}

// Helper functions for finding and validating resources.

// findHardwareProfileByName searches for a HardwareProfile by name in the list.
// Returns the profile and true if found, nil and false otherwise.
func findHardwareProfileByName(hwpList *infrav1.HardwareProfileList, name string) (*infrav1.HardwareProfile, bool) {
	for i := range hwpList.Items {
		if hwpList.Items[i].Name == name {
			return &hwpList.Items[i], true
		}
	}
	return nil, false
}

// validateAcceleratorProfileHardwareProfile validates that a HardwareProfile created from an AcceleratorProfile
// has the correct structure and values based on the input AcceleratorProfile and OdhDashboardConfig.
func validateAcceleratorProfileHardwareProfile(g *WithT, hwp *infrav1.HardwareProfile,
	ap *unstructured.Unstructured, odhConfig *unstructured.Unstructured, profileType string) {
	// Extract AP fields
	apName := ap.GetName()
	apNamespace := ap.GetNamespace()
	apSpec, found, err := unstructured.NestedMap(ap.Object, "spec")
	g.Expect(err).ShouldNot(HaveOccurred(), "failed to get spec from AcceleratorProfile")
	g.Expect(found).To(BeTrue(), "spec not found in AcceleratorProfile")

	identifier, _ := apSpec["identifier"].(string)
	displayName, _ := apSpec["displayName"].(string)
	description, _ := apSpec["description"].(string)
	enabled, _ := apSpec["enabled"].(bool)
	apAnnotations := ap.GetAnnotations()

	// Validate TypeMeta
	g.Expect(hwp.TypeMeta.APIVersion).To(Equal(infrav1.GroupVersion.String()))
	g.Expect(hwp.TypeMeta.Kind).To(Equal("HardwareProfile"))

	// Validate ObjectMeta
	g.Expect(hwp.GetName()).To(Equal(fmt.Sprintf("%s-%s", apName, profileType)))
	g.Expect(hwp.GetNamespace()).To(Equal(apNamespace))

	// Validate annotations
	annotations := hwp.GetAnnotations()
	g.Expect(annotations).ToNot(BeNil())
	g.Expect(annotations).To(HaveKey("opendatahub.io/dashboard-feature-visibility"))
	expectedVisibility := `["model-serving"]`
	if profileType == notebooksProfileType {
		expectedVisibility = `["workbench"]`
	}
	g.Expect(annotations["opendatahub.io/dashboard-feature-visibility"]).To(Equal(expectedVisibility))
	g.Expect(annotations).To(HaveKey("opendatahub.io/modified-date"))
	g.Expect(annotations).To(HaveKey("opendatahub.io/display-name"))
	g.Expect(annotations["opendatahub.io/display-name"]).To(Equal(displayName))
	g.Expect(annotations).To(HaveKey("opendatahub.io/description"))
	g.Expect(annotations["opendatahub.io/description"]).To(Equal(description))
	g.Expect(annotations).To(HaveKey("opendatahub.io/disabled"))
	g.Expect(annotations["opendatahub.io/disabled"]).To(Equal(strconv.FormatBool(!enabled)))
	// All AP annotations should be present in HWP
	for k, v := range apAnnotations {
		g.Expect(annotations).To(HaveKeyWithValue(k, v))
	}

	// Validate identifiers
	g.Expect(hwp.Spec.Identifiers).To(HaveLen(expectedAcceleratorProfileIdentifierCount))

	// Find accelerator, cpu, and memory identifiers
	var acceleratorIdentifier *infrav1.HardwareIdentifier
	var cpuIdentifier *infrav1.HardwareIdentifier
	var memoryIdentifier *infrav1.HardwareIdentifier

	for i := range hwp.Spec.Identifiers {
		id := &hwp.Spec.Identifiers[i]
		switch id.ResourceType {
		case "Accelerator":
			acceleratorIdentifier = id
		case "CPU":
			cpuIdentifier = id
		case "Memory":
			memoryIdentifier = id
		}
	}

	// Validate accelerator identifier
	g.Expect(acceleratorIdentifier).ToNot(BeNil())
	g.Expect(acceleratorIdentifier.Identifier).To(Equal(identifier))
	g.Expect(acceleratorIdentifier.DisplayName).To(Equal(identifier))
	g.Expect(acceleratorIdentifier.ResourceType).To(Equal("Accelerator"))
	g.Expect(acceleratorIdentifier.MinCount).To(Equal(intstr.FromInt(1)))
	g.Expect(acceleratorIdentifier.DefaultCount).To(Equal(intstr.FromInt(1)))

	// Validate CPU identifier
	g.Expect(cpuIdentifier).ToNot(BeNil())
	g.Expect(cpuIdentifier.Identifier).To(Equal("cpu"))
	g.Expect(cpuIdentifier.DisplayName).To(Equal("cpu"))
	g.Expect(cpuIdentifier.ResourceType).To(Equal("CPU"))

	// Validate Memory identifier
	g.Expect(memoryIdentifier).ToNot(BeNil())
	g.Expect(memoryIdentifier.Identifier).To(Equal("memory"))
	g.Expect(memoryIdentifier.DisplayName).To(Equal("memory"))
	g.Expect(memoryIdentifier.ResourceType).To(Equal("Memory"))

	if profileType == notebooksProfileType {
		// containerCounts, which is calculated from odhConfig
		containerCounts, err := upgrade.FindContainerCpuMemoryMinMaxCount(odhConfig, "notebookSizes")
		g.Expect(err).ShouldNot(HaveOccurred(), "failed to find container CPU/Memory counts")

		// minCpu
		expectedMinCpu := intstr.FromString(containerCounts["minCpu"])
		g.Expect(cpuIdentifier.MinCount).To(Equal(expectedMinCpu))
		g.Expect(cpuIdentifier.DefaultCount).To(Equal(expectedMinCpu))

		// maxCpu
		if maxCpu, ok := containerCounts["maxCpu"]; ok && maxCpu != "" {
			g.Expect(cpuIdentifier.MaxCount).ToNot(BeNil())
			g.Expect(cpuIdentifier.MaxCount.Type).To(Equal(intstr.String))
			g.Expect(cpuIdentifier.MaxCount.StrVal).To(Equal(maxCpu))
		}

		// minMemory
		expectedMinMemory := intstr.FromString(containerCounts["minMemory"])
		g.Expect(memoryIdentifier.MinCount).To(Equal(expectedMinMemory))
		g.Expect(memoryIdentifier.DefaultCount).To(Equal(expectedMinMemory))

		// maxMemory
		if maxMemory, ok := containerCounts["maxMemory"]; ok && maxMemory != "" {
			g.Expect(memoryIdentifier.MaxCount).ToNot(BeNil())
			g.Expect(memoryIdentifier.MaxCount.Type).To(Equal(intstr.String))
			g.Expect(memoryIdentifier.MaxCount.StrVal).To(Equal(maxMemory))
		}
	} else {
		minCpu := intstr.FromString("1")
		g.Expect(cpuIdentifier.MinCount).To(Equal(minCpu))
		g.Expect(cpuIdentifier.DefaultCount).To(Equal(minCpu))
		minMem := intstr.FromString("1Gi")
		g.Expect(memoryIdentifier.MinCount).To(Equal(minMem))
		g.Expect(memoryIdentifier.DefaultCount).To(Equal(minMem))
	}

	// Validate scheduling spec
	if hwp.Spec.SchedulingSpec != nil && hwp.Spec.SchedulingSpec.Node != nil && len(hwp.Spec.SchedulingSpec.Node.Tolerations) > 0 {
		g.Expect(hwp.Spec.SchedulingSpec.SchedulingType).To(Equal(infrav1.NodeScheduling))
		g.Expect(hwp.Spec.SchedulingSpec.Node).ToNot(BeNil())
	}

	// Validate tolerations from AP
	apTolerations, _, err := unstructured.NestedSlice(ap.Object, "spec", "tolerations")
	g.Expect(err).ShouldNot(HaveOccurred(), "failed to get tolerations from AcceleratorProfile")
	// Note: Tolerations are optional, so not finding them is acceptable

	if hwp.Spec.SchedulingSpec != nil && hwp.Spec.SchedulingSpec.Node != nil {
		for _, apTol := range apTolerations {
			apTolMap, ok := apTol.(map[string]interface{})
			if !ok {
				continue
			}
			apTolKey, _ := apTolMap["key"].(string)
			apTolValue, _ := apTolMap["value"].(string)
			apTolOperator, _ := apTolMap["operator"].(string)
			apTolEffect, _ := apTolMap["effect"].(string)

			foundMatchingToleration := false
			for _, hwpTol := range hwp.Spec.SchedulingSpec.Node.Tolerations {
				if hwpTol.Key == apTolKey &&
					hwpTol.Value == apTolValue &&
					string(hwpTol.Operator) == apTolOperator &&
					string(hwpTol.Effect) == apTolEffect {
					foundMatchingToleration = true
					break
				}
			}
			g.Expect(foundMatchingToleration).To(BeTrue(), "AP toleration should be present in HWP")
		}
	}
	validateNotebooksOnlyToleration(g, hwp, odhConfig, profileType)
}

func validateNotebooksOnlyToleration(g *WithT, hwp *infrav1.HardwareProfile, odhConfig *unstructured.Unstructured, profileType string) {
	// Validate notebooks-only toleration for notebooks profile
	if profileType != notebooksProfileType {
		return
	}
	if hwp.Spec.SchedulingSpec == nil || hwp.Spec.SchedulingSpec.Node == nil {
		return
	}

	notebookController, found, err := unstructured.NestedMap(odhConfig.Object, "spec", "notebookController")
	g.Expect(err).ShouldNot(HaveOccurred(), "failed to get notebookController from OdhDashboardConfig")
	if !found || notebookController == nil {
		return
	}

	enabled, found, err := unstructured.NestedBool(notebookController, "enabled")
	g.Expect(err).ShouldNot(HaveOccurred(), "failed to get enabled from notebookController")
	if !found || !enabled {
		return
	}

	tolerationSettings, found, err := unstructured.NestedMap(notebookController, "notebookTolerationSettings")
	g.Expect(err).ShouldNot(HaveOccurred(), "failed to get notebookTolerationSettings")
	if !found || tolerationSettings == nil {
		return
	}

	tolerationEnabled, found, err := unstructured.NestedBool(tolerationSettings, "enabled")
	g.Expect(err).ShouldNot(HaveOccurred(), "failed to get enabled from notebookTolerationSettings")
	if !found || !tolerationEnabled {
		return
	}

	key, found, err := unstructured.NestedString(tolerationSettings, "key")
	g.Expect(err).ShouldNot(HaveOccurred(), "failed to get key from notebookTolerationSettings")
	if !found || key == "" {
		return
	}

	value, _, valueErr := unstructured.NestedString(tolerationSettings, "value")
	g.Expect(valueErr).ShouldNot(HaveOccurred(), "failed to get value from notebookTolerationSettings")

	operator, _, operatorErr := unstructured.NestedString(tolerationSettings, "operator")
	g.Expect(operatorErr).ShouldNot(HaveOccurred(), "failed to get operator from notebookTolerationSettings")

	effect, _, effectErr := unstructured.NestedString(tolerationSettings, "effect")
	g.Expect(effectErr).ShouldNot(HaveOccurred(), "failed to get effect from notebookTolerationSettings")

	foundToleration := false
	for _, tol := range hwp.Spec.SchedulingSpec.Node.Tolerations {
		if tol.Key == key &&
			(value == "" || tol.Value == value) &&
			(operator == "" || string(tol.Operator) == operator) &&
			(effect == "" || string(tol.Effect) == effect) {
			foundToleration = true
			break
		}
	}
	g.Expect(foundToleration).To(BeTrue(), "Should have notebooks-only toleration")
}

// validateContainerSizeHardwareProfile validates that a HardwareProfile created from container sizes
// has the correct structure and values based on the input container size and OdhDashboardConfig.
func validateContainerSizeHardwareProfile(g *WithT, hwp *infrav1.HardwareProfile,
	containerSize map[string]interface{}, odhConfig *unstructured.Unstructured, sizeType string) {
	// Extract container size fields
	sizeName, _ := containerSize["name"].(string)
	resources, _ := containerSize["resources"].(map[string]interface{})
	requests, _ := resources["requests"].(map[string]interface{})
	limits, _ := resources["limits"].(map[string]interface{})

	// Validate TypeMeta
	g.Expect(hwp.TypeMeta.APIVersion).To(Equal(infrav1.GroupVersion.String()))
	g.Expect(hwp.TypeMeta.Kind).To(Equal("HardwareProfile"))

	// Validate ObjectMeta
	expectedName := fmt.Sprintf("containerSize-%s-%s", sizeName, sizeType)
	expectedName = strings.ReplaceAll(strings.ToLower(expectedName), " ", "-")
	g.Expect(hwp.GetName()).To(Equal(expectedName))
	g.Expect(hwp.GetNamespace()).ToNot(BeEmpty()) // Should be set to application namespace

	// Validate annotations
	annotations := hwp.GetAnnotations()
	g.Expect(annotations).ToNot(BeNil())
	g.Expect(annotations).To(HaveKey("opendatahub.io/dashboard-feature-visibility"))
	expectedVisibility := `["model-serving"]`
	if sizeType == notebooksProfileType {
		expectedVisibility = `["workbench"]`
	}
	g.Expect(annotations["opendatahub.io/dashboard-feature-visibility"]).To(Equal(expectedVisibility))
	g.Expect(annotations).To(HaveKey("opendatahub.io/modified-date"))
	g.Expect(annotations).To(HaveKey("opendatahub.io/display-name"))
	g.Expect(annotations["opendatahub.io/display-name"]).To(Equal(sizeName))
	g.Expect(annotations).To(HaveKey("opendatahub.io/description"))
	g.Expect(annotations["opendatahub.io/description"]).To(Equal(""))
	g.Expect(annotations).To(HaveKey("opendatahub.io/disabled"))
	g.Expect(annotations["opendatahub.io/disabled"]).To(Equal("false"))

	// Validate identifiers
	g.Expect(hwp.Spec.Identifiers).To(HaveLen(expectedContainerSizeIdentifierCount))

	// Find CPU and memory identifiers
	var cpuIdentifier *infrav1.HardwareIdentifier
	var memoryIdentifier *infrav1.HardwareIdentifier

	for i := range hwp.Spec.Identifiers {
		id := &hwp.Spec.Identifiers[i]
		switch id.ResourceType {
		case "CPU":
			cpuIdentifier = id
		case "Memory":
			memoryIdentifier = id
		}
	}

	// Validate CPU identifier
	g.Expect(cpuIdentifier).ToNot(BeNil())
	g.Expect(cpuIdentifier.Identifier).To(Equal("cpu"))
	g.Expect(cpuIdentifier.DisplayName).To(Equal("cpu"))
	g.Expect(cpuIdentifier.ResourceType).To(Equal("CPU"))

	// CPU MinCount and DefaultCount should be based on requests
	requestCpu, _ := requests["cpu"].(string)
	limitCpu, _ := limits["cpu"].(string)
	g.Expect(cpuIdentifier.MinCount).To(Equal(intstr.FromString(requestCpu)))
	g.Expect(cpuIdentifier.DefaultCount).To(Equal(intstr.FromString(requestCpu)))

	// CPU MaxCount should be based on limits
	if limitCpu != "" {
		g.Expect(cpuIdentifier.MaxCount).ToNot(BeNil())
		g.Expect(cpuIdentifier.MaxCount.Type).To(Equal(intstr.String))
		g.Expect(cpuIdentifier.MaxCount.StrVal).To(Equal(limitCpu))
	}

	// Validate Memory identifier
	g.Expect(memoryIdentifier).ToNot(BeNil())
	g.Expect(memoryIdentifier.Identifier).To(Equal("memory"))
	g.Expect(memoryIdentifier.DisplayName).To(Equal("memory"))
	g.Expect(memoryIdentifier.ResourceType).To(Equal("Memory"))

	// Memory MinCount and DefaultCount should be based on requests
	requestMemory, _ := requests["memory"].(string)
	limitMemory, _ := limits["memory"].(string)
	g.Expect(memoryIdentifier.MinCount).To(Equal(intstr.FromString(requestMemory)))
	g.Expect(memoryIdentifier.DefaultCount).To(Equal(intstr.FromString(requestMemory)))

	// Memory MaxCount should be based on limits
	if limitMemory != "" {
		g.Expect(memoryIdentifier.MaxCount).ToNot(BeNil())
		g.Expect(memoryIdentifier.MaxCount.Type).To(Equal(intstr.String))
		g.Expect(memoryIdentifier.MaxCount.StrVal).To(Equal(limitMemory))
	}

	// Validate scheduling spec
	if hwp.Spec.SchedulingSpec != nil {
		g.Expect(hwp.Spec.SchedulingSpec.SchedulingType).To(Equal(infrav1.NodeScheduling))
		g.Expect(hwp.Spec.SchedulingSpec.Node).ToNot(BeNil())
		g.Expect(hwp.Spec.SchedulingSpec.Node.Tolerations).ToNot(BeNil())
	}

	// Validate notebooks-only toleration for notebooks size type
	if sizeType == "notebooks" {
		validateNotebooksOnlyToleration(g, hwp, odhConfig, sizeType)
	}
}

// runNotebookHWPMigrationTest is a helper function to test notebook HWP annotation migration.
// It creates a notebook with the given annotations, runs the migration, and verifies the expected HWP annotation.
func runNotebookHWPMigrationTest(t *testing.T, ctx context.Context, namespace, notebookName string,
	initialAnnotations map[string]string, expectedHWPName string) {
	t.Helper()
	g := NewWithT(t)

	odhConfig := createTestOdhDashboardConfig(t, namespace)
	notebook := createTestNotebook(namespace, notebookName)
	notebook.SetAnnotations(initialAnnotations)

	cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, notebook))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify HWP annotation was added
	updatedNotebook := &unstructured.Unstructured{}
	updatedNotebook.SetGroupVersionKind(gvk.Notebook)
	err = cli.Get(ctx, client.ObjectKey{Name: notebookName, Namespace: namespace}, updatedNotebook)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(updatedNotebook.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", expectedHWPName))
}

// ClusterState represents a snapshot of cluster resources for idempotence verification.
// This struct deliberately tracks ONLY the resources modified by MigrateToInfraHardwareProfiles:
//   - HardwareProfiles: Created during migration from AcceleratorProfiles and container sizes
//   - NotebookAnnotations: Updated with hardware-profile-name and hardware-profile-namespace annotations
//   - ISVCAnnotations: Updated with hardware-profile-name and hardware-profile-namespace annotations
//
// Other resources managed by CleanupExistingResource (RoleBindings, Deployments, etc.) are intentionally
// excluded as they are not relevant to the HardwareProfile migration idempotence tests.
type ClusterState struct {
	HardwareProfiles    []infrav1.HardwareProfile
	NotebookAnnotations map[string]map[string]string // Map: notebook name -> annotations
	ISVCAnnotations     map[string]map[string]string // Map: ISVC name -> annotations
}

// captureClusterState captures the current state of resources modified by MigrateToInfraHardwareProfiles.
// This function lists all HardwareProfiles in the specified namespace and captures annotations from
// all Notebooks and InferenceServices (across all namespaces) for comparison.
//
// Returns:
//   - *ClusterState: Snapshot of current cluster state
//   - error: Any errors encountered during state capture (except NoMatchError which is ignored)
func captureClusterState(ctx context.Context, cli client.Client, namespace string) (*ClusterState, error) {
	state := &ClusterState{
		NotebookAnnotations: make(map[string]map[string]string),
		ISVCAnnotations:     make(map[string]map[string]string),
	}

	// Capture HardwareProfiles
	var hwpList infrav1.HardwareProfileList
	if err := cli.List(ctx, &hwpList, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("failed to list HardwareProfiles: %w", err)
	}
	state.HardwareProfiles = hwpList.Items

	// Capture Notebook annotations
	notebookList := &unstructured.UnstructuredList{}
	notebookList.SetGroupVersionKind(gvk.Notebook)
	if err := cli.List(ctx, notebookList, client.InNamespace(namespace)); err != nil && !meta.IsNoMatchError(err) {
		return nil, fmt.Errorf("failed to list notebooks: %w", err)
	}
	for i := range notebookList.Items {
		nb := &notebookList.Items[i]
		annotations := nb.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		state.NotebookAnnotations[nb.GetName()] = annotations
	}

	// Capture InferenceService annotations
	isvcList := &unstructured.UnstructuredList{}
	isvcList.SetGroupVersionKind(gvk.InferenceServices)
	if err := cli.List(ctx, isvcList, client.InNamespace(namespace)); err != nil && !meta.IsNoMatchError(err) {
		return nil, fmt.Errorf("failed to list InferenceServices: %w", err)
	}
	for i := range isvcList.Items {
		isvc := &isvcList.Items[i]
		annotations := isvc.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		state.ISVCAnnotations[isvc.GetName()] = annotations
	}

	return state, nil
}

// compareClusterStates performs a deep comparison of two cluster states for idempotence verification.
// This function checks that running the migration multiple times produces identical results.
//
// Comparison logic:
//   - HardwareProfiles: Compares count, names, and specs using reflect.DeepEqual
//   - Notebook/ISVC annotations: Compares all annotations except timestamps (opendatahub.io/modified-date)
//
// Returns:
//   - bool: true if states are identical (idempotent), false if differences found
//   - []string: List of specific differences found (empty if identical)
func compareClusterStates(before, after *ClusterState) (bool, []string) {
	var differences []string

	// Compare HardwareProfiles count and names
	if len(before.HardwareProfiles) != len(after.HardwareProfiles) {
		differences = append(differences, fmt.Sprintf("HardwareProfile count changed: %d -> %d",
			len(before.HardwareProfiles), len(after.HardwareProfiles)))
	}

	// Create maps for quick lookup and deep comparison
	beforeHWPMap := make(map[string]*infrav1.HardwareProfile)
	for i := range before.HardwareProfiles {
		hwp := &before.HardwareProfiles[i]
		beforeHWPMap[hwp.Name] = hwp
	}
	afterHWPMap := make(map[string]*infrav1.HardwareProfile)
	for i := range after.HardwareProfiles {
		hwp := &after.HardwareProfiles[i]
		afterHWPMap[hwp.Name] = hwp
	}

	// Check for disappeared HardwareProfiles
	for name := range beforeHWPMap {
		if _, exists := afterHWPMap[name]; !exists {
			differences = append(differences, fmt.Sprintf("HardwareProfile %s disappeared", name))
		}
	}

	// Check for appeared HardwareProfiles
	for name := range afterHWPMap {
		if _, exists := beforeHWPMap[name]; !exists {
			differences = append(differences, fmt.Sprintf("HardwareProfile %s appeared", name))
		}
	}

	// Deep compare specs for HardwareProfiles that exist in both states
	for name, beforeHWP := range beforeHWPMap {
		if afterHWP, exists := afterHWPMap[name]; exists {
			if !reflect.DeepEqual(beforeHWP.Spec, afterHWP.Spec) {
				differences = append(differences, fmt.Sprintf("HardwareProfile %s spec changed", name))
			}
		}
	}

	// Compare Notebook annotations (excluding timestamp annotations)
	notebookDiffs := compareAnnotationMaps("Notebook", before.NotebookAnnotations, after.NotebookAnnotations)
	differences = append(differences, notebookDiffs...)

	// Compare InferenceService annotations (excluding timestamp annotations)
	isvcDiffs := compareAnnotationMaps("InferenceService", before.ISVCAnnotations, after.ISVCAnnotations)
	differences = append(differences, isvcDiffs...)

	return len(differences) == 0, differences
}

// compareAnnotationMaps compares annotations on resources (Notebooks or InferenceServices), ignoring timestamps.
// This function provides detailed difference reporting for idempotence verification.
//
// Parameters:
//   - resourceType: Type of resource being compared (e.g., "Notebook", "InferenceService")
//   - before: Map of resource name to annotations from first state capture
//   - after: Map of resource name to annotations from second state capture
//
// Returns:
//   - []string: List of specific differences with details (added/removed/changed annotations)
//
// The function ignores the "opendatahub.io/modified-date" annotation as it changes on every update.
func compareAnnotationMaps(resourceType string, before, after map[string]map[string]string) []string {
	var differences []string

	// Check for resources that disappeared
	for name := range before {
		if _, exists := after[name]; !exists {
			differences = append(differences, fmt.Sprintf("%s %s disappeared", resourceType, name))
		}
	}

	// Check for resources that appeared
	for name := range after {
		if _, exists := before[name]; !exists {
			differences = append(differences, fmt.Sprintf("%s %s appeared", resourceType, name))
		}
	}

	// Compare annotations for resources that exist in both states
	for name, beforeAnnotations := range before {
		afterAnnotations, exists := after[name]
		if !exists {
			continue // Already reported as disappeared
		}

		// Compare annotations excluding timestamps
		for key, beforeValue := range beforeAnnotations {
			// Skip timestamp annotations
			if key == "opendatahub.io/modified-date" {
				continue
			}

			afterValue, exists := afterAnnotations[key]
			if !exists {
				differences = append(differences, fmt.Sprintf("%s %s annotation %s was removed", resourceType, name, key))
			} else if beforeValue != afterValue {
				differences = append(differences, fmt.Sprintf("%s %s annotation %s changed: %q -> %q", resourceType, name, key, beforeValue, afterValue))
			}
		}

		// Check for new annotations (excluding timestamps)
		for key, afterValue := range afterAnnotations {
			if key == "opendatahub.io/modified-date" {
				continue
			}
			if _, exists := beforeAnnotations[key]; !exists {
				differences = append(differences, fmt.Sprintf("%s %s annotation %s was added: %q", resourceType, name, key, afterValue))
			}
		}
	}

	return differences
}

// GatewayConfigState represents a snapshot of GatewayConfig state for idempotence verification.
type GatewayConfigState struct {
	Exists      bool
	IngressMode string
}

// captureGatewayConfigState captures the current state of the GatewayConfig resource.
// Returns the ingressMode value and whether the GatewayConfig exists.
func captureGatewayConfigState(ctx context.Context, cli client.Client) (*GatewayConfigState, error) {
	state := &GatewayConfigState{}

	gatewayConfig := &unstructured.Unstructured{}
	gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)

	err := cli.Get(ctx, client.ObjectKey{Name: "default-gateway"}, gatewayConfig)
	if k8serr.IsNotFound(err) {
		state.Exists = false
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get GatewayConfig: %w", err)
	}

	state.Exists = true
	// Field may not exist, which is a valid state (ingressMode not yet set)
	ingressMode, _, err := unstructured.NestedString(gatewayConfig.Object, "spec", "ingressMode")
	if err != nil {
		return nil, fmt.Errorf("failed to get ingressMode from GatewayConfig: %w", err)
	}
	state.IngressMode = ingressMode

	return state, nil
}

// compareGatewayConfigStates compares two GatewayConfig states for idempotence verification.
// Returns true if states are identical, false otherwise with a list of differences.
func compareGatewayConfigStates(before, after *GatewayConfigState) (bool, []string) {
	var differences []string

	if before.Exists != after.Exists {
		differences = append(differences, fmt.Sprintf("GatewayConfig existence changed: %v -> %v", before.Exists, after.Exists))
	}

	if before.IngressMode != after.IngressMode {
		differences = append(differences, fmt.Sprintf("ingressMode changed: %q -> %q", before.IngressMode, after.IngressMode))
	}

	return len(differences) == 0, differences
}

// verifyGatewayConfigIdempotence runs the migration multiple times and verifies state remains identical.
// It captures state after each run and compares them. If an initialState is provided, the first run is compared
// against it; otherwise the first run establishes the baseline for subsequent comparisons.
func verifyGatewayConfigIdempotence(ctx context.Context, g *WithT, cli client.Client, runs int, initialState *GatewayConfigState) {
	previousState := initialState
	for i := range runs {
		err := upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred(), "Run %d should complete without errors", i+1)

		currentState, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		if previousState != nil {
			identical, differences := compareGatewayConfigStates(previousState, currentState)
			g.Expect(identical).To(BeTrue(), "Run %d: States should be identical. Differences: %v", i+1, differences)
		}
		previousState = currentState
	}
}

// verifyClusterStateIdempotence runs the migration multiple times and verifies cluster state remains identical.
// It captures state after each run and compares them. If an initialState is provided, the first run is compared
// against it; otherwise the first run establishes the baseline for subsequent comparisons.
func verifyClusterStateIdempotence(ctx context.Context, g *WithT, cli client.Client, namespace string, runs int, initialState *ClusterState) {
	previousState := initialState
	for i := range runs {
		err := upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred(), "Run %d should complete without errors", i+1)

		currentState, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		if previousState != nil {
			identical, differences := compareClusterStates(previousState, currentState)
			g.Expect(identical).To(BeTrue(), "Run %d: States should be identical. Differences: %v", i+1, differences)
		}
		previousState = currentState
	}
}
