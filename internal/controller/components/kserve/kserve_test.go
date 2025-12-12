//nolint:testpackage,dupl
package kserve

import (
	"encoding/json"
	"testing"

	gt "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentName))
}

func TestNewCRObject(t *testing.T) {
	handler := &componentHandler{}

	g := NewWithT(t)
	dsc := createDSCWithKserve(operatorv1.Managed)

	cr := handler.NewCRObject(dsc)
	g.Expect(cr).ShouldNot(BeNil())
	g.Expect(cr).Should(BeAssignableToTypeOf(&componentApi.Kserve{}))

	g.Expect(cr).Should(WithTransform(json.Marshal, And(
		jq.Match(`.metadata.name == "%s"`, componentApi.KserveInstanceName),
		jq.Match(`.kind == "%s"`, componentApi.KserveKind),
		jq.Match(`.apiVersion == "%s"`, componentApi.GroupVersion),
		jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func TestIsEnabled(t *testing.T) {
	handler := &componentHandler{}

	tests := []struct {
		name    string
		state   operatorv1.ManagementState
		matcher gt.GomegaMatcher
	}{
		{
			name:    "should return true when management state is Managed",
			state:   operatorv1.Managed,
			matcher: BeTrue(),
		},
		{
			name:    "should return false when management state is Removed",
			state:   operatorv1.Removed,
			matcher: BeFalse(),
		},
		{
			name:    "should return false when management state is Unmanaged",
			state:   operatorv1.Unmanaged,
			matcher: BeFalse(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dsc := createDSCWithKserve(tt.state)

			g.Expect(
				handler.IsEnabled(dsc),
			).Should(
				tt.matcher,
			)
		})
	}
}

func TestUpdateDSCStatus(t *testing.T) {
	handler := &componentHandler{}

	t.Run("should handle enabled component with ready Kserve CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKserve(operatorv1.Managed)
		kserve := createKserveCR(true)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, kserve))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionTrue))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.kserve.managementState == "%s"`, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.ReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is ready"`, ReadyConditionType)),
		))
	})

	t.Run("should handle enabled component with not ready Kserve CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKserve(operatorv1.Managed)
		kserve := createKserveCR(false)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, kserve))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.kserve.managementState == "%s"`, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is not ready"`, ReadyConditionType)),
		))
	})

	t.Run("should handle disabled component", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKserve(operatorv1.Removed)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.kserve.managementState == "%s"`, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})

	t.Run("should handle empty management state as Removed", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKserve("")

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.kserve.managementState == "%s"`, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})

	t.Run("should propagate KServe conditions to DSC", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()
		dsc := createDSCWithKserve(operatorv1.Managed)
		kserve := createKserveCR(true)
		kserve.Status.Conditions = append(kserve.Status.Conditions, common.Condition{
			Type:   LLMInferenceServiceDependencies,
			Status: metav1.ConditionTrue,
		})

		kserve.Status.Conditions = append(kserve.Status.Conditions, common.Condition{
			Type:   LLMInferenceServiceWideEPDependencies,
			Status: metav1.ConditionFalse,
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, kserve))
		g.Expect(err).ShouldNot(HaveOccurred())
		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionTrue))
		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, LLMInferenceServiceDependencies, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, LLMInferenceServiceWideEPDependencies, metav1.ConditionFalse),
		)))
	})
}

func createDSCWithKserve(managementState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.Kserve.ManagementState = managementState

	return &dsc
}

func createKserveCR(ready bool) *componentApi.Kserve {
	c := componentApi.Kserve{}
	c.SetGroupVersionKind(gvk.Kserve)
	c.SetName(componentApi.KserveInstanceName)

	if ready {
		c.Status.Conditions = []common.Condition{{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  status.ReadyReason,
			Message: "Component is ready",
		}}
	} else {
		c.Status.Conditions = []common.Condition{{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: "Component is not ready",
		}}
	}

	return &c
}

func TestVersionedWellKnownLLMInferenceServiceConfigs(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		resourceName   string
		isWellKnown    bool
		expectedName   string
		expectedEnvVar string
	}{
		{
			name:           "should version well-known LLMInferenceServiceConfig",
			version:        "v2-25-0",
			resourceName:   "kserve-config-llm-decode-template",
			isWellKnown:    true,
			expectedName:   "v2-25-0-kserve-config-llm-decode-template",
			expectedEnvVar: "v2-25-0-kserve-",
		},
		{
			name:           "should not version non-well-known LLMInferenceServiceConfig",
			version:        "v2-25-0",
			resourceName:   "custom-config",
			isWellKnown:    false,
			expectedName:   "custom-config",
			expectedEnvVar: "v2-25-0-kserve-",
		},
		{
			name:           "should handle different version format",
			version:        "v1-0-0",
			resourceName:   "kserve-config-llm-prefill-template",
			isWellKnown:    true,
			expectedName:   "v1-0-0-kserve-config-llm-prefill-template",
			expectedEnvVar: "v1-0-0-kserve-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			// Create reconciliation request with LLMInferenceServiceConfig resource
			resource := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "serving.kserve.io/v1alpha1",
					"kind":       "LLMInferenceServiceConfig",
					"metadata": map[string]interface{}{
						"name": tt.resourceName,
					},
				},
			}

			// Add well-known annotation if applicable
			if tt.isWellKnown {
				resource.SetAnnotations(map[string]string{
					LLMInferenceServiceConfigWellKnownAnnotationKey: LLMInferenceServiceConfigWellKnownAnnotationValue,
				})
			}

			rr := &types.ReconciliationRequest{
				Resources: []unstructured.Unstructured{*resource},
			}

			// Call the versioning function
			err := versionedWellKnownLLMInferenceServiceConfigs(ctx, tt.version, rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify the resource name
			g.Expect(rr.Resources[0].GetName()).Should(Equal(tt.expectedName))
		})
	}

	t.Run("should inject LLM_INFERENCE_SERVICE_CONFIG_PREFIX env var into deployments", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		version := "v2-25-0"
		expectedEnvValue := "v2-25-0-kserve-"

		// Create reconciliation request with Deployment resource
		resource := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name": "kserve-controller",
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "manager",
									"image": "kserve/controller:latest",
									"env":   []interface{}{},
								},
							},
						},
					},
				},
			},
		}

		rr := &types.ReconciliationRequest{
			Resources: []unstructured.Unstructured{*resource},
		}

		// Call the versioning function
		err := versionedWellKnownLLMInferenceServiceConfigs(ctx, version, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify the env var was injected by checking the unstructured object directly
		containers, found, err := unstructured.NestedSlice(rr.Resources[0].Object, "spec", "template", "spec", "containers")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(containers).Should(HaveLen(1))

		container, ok := containers[0].(map[string]interface{})
		g.Expect(ok).Should(BeTrue(), "container should be a map")
		env, found, err := unstructured.NestedSlice(container, "env")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(env).Should(HaveLen(1))

		envVar, ok := env[0].(map[string]interface{})
		g.Expect(ok).Should(BeTrue(), "envVar should be a map")
		g.Expect(envVar["name"]).Should(Equal("LLM_INFERENCE_SERVICE_CONFIG_PREFIX"))
		g.Expect(envVar["value"]).Should(Equal(expectedEnvValue))
	})

	t.Run("should update existing LLM_INFERENCE_SERVICE_CONFIG_PREFIX env var in deployments", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		version := "v3-0-0"
		expectedEnvValue := "v3-0-0-kserve-"

		// Create reconciliation request with Deployment resource with existing env var
		resource := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name": "kserve-controller",
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "manager",
									"image": "kserve/controller:latest",
									"env": []interface{}{
										map[string]interface{}{
											"name":  "LLM_INFERENCE_SERVICE_CONFIG_PREFIX",
											"value": "old-prefix-",
										},
									},
								},
							},
						},
					},
				},
			},
		}

		rr := &types.ReconciliationRequest{
			Resources: []unstructured.Unstructured{*resource},
		}

		// Call the versioning function
		err := versionedWellKnownLLMInferenceServiceConfigs(ctx, version, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify the env var was updated by checking the unstructured object directly
		containers, found, err := unstructured.NestedSlice(rr.Resources[0].Object, "spec", "template", "spec", "containers")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(containers).Should(HaveLen(1))

		container, ok := containers[0].(map[string]interface{})
		g.Expect(ok).Should(BeTrue(), "container should be a map")
		env, found, err := unstructured.NestedSlice(container, "env")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(env).Should(HaveLen(1))

		envVar, ok := env[0].(map[string]interface{})
		g.Expect(ok).Should(BeTrue(), "envVar should be a map")
		g.Expect(envVar["name"]).Should(Equal("LLM_INFERENCE_SERVICE_CONFIG_PREFIX"))
		g.Expect(envVar["value"]).Should(Equal(expectedEnvValue))
	})
}
