//nolint:testpackage
package kserve

import (
	"encoding/json"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	// Test constants for ServiceMesh operator resources.
	testOperatorNamespace = "openshift-operators"
)

func TestCustomizeKserveConfigMap(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	t.Run("Test KServe default config: RawDeployment mode + headless", func(t *testing.T) {
		// KServe instance to be created with default (headless) config
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{},
			},
		}

		initialConfigMap := createTestConfigMap()
		initialDeployment := createTestDeployment()
		resources := []unstructured.Unstructured{
			*convertToUnstructured(t, initialConfigMap),
			*convertToUnstructured(t, initialDeployment),
		}

		rr := &odhtypes.ReconciliationRequest{
			Instance:  kserve,
			Resources: resources,
		}

		err := customizeKserveConfigMap(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		updatedConfigMap := &corev1.ConfigMap{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(rr.Resources[0].Object, updatedConfigMap)
		g.Expect(err).ShouldNot(HaveOccurred())

		// verify ingress creation is disabled
		var ingressData map[string]interface{}
		err = json.Unmarshal([]byte(updatedConfigMap.Data[IngressConfigKeyName]), &ingressData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ingressData["disableIngressCreation"]).Should(BeTrue())

		// verify service is configured as headless (default)
		var serviceData map[string]interface{}
		err = json.Unmarshal([]byte(updatedConfigMap.Data[ServiceConfigKeyName]), &serviceData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(serviceData["serviceClusterIPNone"]).Should(BeTrue())
	})

	t.Run("Test KServe config: RawDeployment mode + headed", func(t *testing.T) {
		// create a KServe instance with headed service config
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					RawDeploymentServiceConfig: componentApi.KserveRawHeaded,
				},
			},
		}

		initialConfigMap := createTestConfigMap()
		initialDeployment := createTestDeployment()
		resources := []unstructured.Unstructured{
			*convertToUnstructured(t, initialConfigMap),
			*convertToUnstructured(t, initialDeployment),
		}

		rr := &odhtypes.ReconciliationRequest{
			Instance:  kserve,
			Resources: resources,
		}

		err := customizeKserveConfigMap(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		updatedConfigMap := &corev1.ConfigMap{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(rr.Resources[0].Object, updatedConfigMap)
		g.Expect(err).ShouldNot(HaveOccurred())

		var ingressData map[string]interface{}
		err = json.Unmarshal([]byte(updatedConfigMap.Data[IngressConfigKeyName]), &ingressData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ingressData["disableIngressCreation"]).Should(BeTrue())

		var serviceData map[string]interface{}
		err = json.Unmarshal([]byte(updatedConfigMap.Data[ServiceConfigKeyName]), &serviceData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(serviceData["serviceClusterIPNone"]).Should(BeFalse())
	})

	t.Run("Test adding ConfigMap hash annotation to deployment", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					RawDeploymentServiceConfig: componentApi.KserveRawHeadless,
				},
			},
		}

		initialConfigMap := createTestConfigMap()
		initialDeployment := createTestDeployment()
		resources := []unstructured.Unstructured{
			*convertToUnstructured(t, initialConfigMap),
			*convertToUnstructured(t, initialDeployment),
		}

		rr := &odhtypes.ReconciliationRequest{
			Instance:  kserve,
			Resources: resources,
		}

		err := customizeKserveConfigMap(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		var updatedDeployment *appsv1.Deployment
		for _, resource := range rr.Resources {
			if resource.GetKind() == "Deployment" && resource.GetName() == "kserve-controller-manager" {
				updatedDeployment = &appsv1.Deployment{}
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, updatedDeployment)
				g.Expect(err).ShouldNot(HaveOccurred())
				break
			}
		}

		g.Expect(updatedDeployment).ShouldNot(BeNil())

		hashAnnotationKey := labels.ODHAppPrefix + "/KserveConfigHash"
		g.Expect(updatedDeployment.Spec.Template.Annotations).Should(HaveKey(hashAnnotationKey))
		g.Expect(updatedDeployment.Spec.Template.Annotations[hashAnnotationKey]).ShouldNot(BeEmpty())
	})

	t.Run("Test KServe ConfigMap not found", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
		}

		// create reconciliation request without the required ConfigMap
		rr := &odhtypes.ReconciliationRequest{
			Instance:  kserve,
			Resources: []unstructured.Unstructured{},
		}

		err := customizeKserveConfigMap(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("could not find"))
		g.Expect(err.Error()).Should(ContainSubstring(kserveConfigMapName))
	})

	t.Run("Test KServe deployment not found", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
		}

		// create reconciliation request with ConfigMap but without deployment
		initialConfigMap := createTestConfigMap()
		resources := []unstructured.Unstructured{
			*convertToUnstructured(t, initialConfigMap),
		}

		rr := &odhtypes.ReconciliationRequest{
			Instance:  kserve,
			Resources: resources,
		}

		err := customizeKserveConfigMap(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("could not find"))
		g.Expect(err.Error()).Should(ContainSubstring("kserve-controller-manager"))
	})
}

func createTestConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: kserveConfigMapName,
		},
		Data: map[string]string{
			IngressConfigKeyName: `{
				"disableIngressCreation": false
			}`,
			ServiceConfigKeyName: `{
				"serviceClusterIPNone": false
			}`,
		},
	}
}

func Test_checkPreConditions(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	dsc := createDSCWithKserve(operatorv1.Managed)
	kserve := createKserveCR(true)

	t.Run("Test rhcl subscription is absent", func(t *testing.T) {
		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, LLMInferenceServiceDependencies),
		}

		err = checkPreConditions(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		cs := rr.Conditions.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(cs.Status).Should(Equal(metav1.ConditionFalse))
	})

	t.Run("Test rhcl subscription is present", func(t *testing.T) {
		rhclSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name: rhclOperatorSubscription,
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, dsc, rhclSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, LLMInferenceServiceDependencies),
		}

		err = checkPreConditions(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		cs := rr.Conditions.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(cs.Status).Should(Equal(metav1.ConditionTrue))
	})

	t.Run("Test only lws subscription is absent", func(t *testing.T) {
		rhclSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name: rhclOperatorSubscription,
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, dsc, rhclSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, LLMInferenceServiceWideEPDependencies),
		}

		err = checkPreConditions(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		cs := rr.Conditions.GetCondition(LLMInferenceServiceWideEPDependencies)
		g.Expect(cs.Status).Should(Equal(metav1.ConditionFalse))
	})

	t.Run("Test when rhcl + lws subscription are present", func(t *testing.T) {
		rhclSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name: rhclOperatorSubscription,
			},
		}
		lwsSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name: lwsOperatorSubscription,
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, dsc, rhclSub, lwsSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, LLMInferenceServiceWideEPDependencies),
		}

		err = checkPreConditions(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		cs := rr.Conditions.GetCondition(LLMInferenceServiceWideEPDependencies)
		g.Expect(cs.Status).Should(Equal(metav1.ConditionTrue))
	})
}

func createTestDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "kserve-controller-manager",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"test-annotation": "test-value",
					},
				},
			},
		},
	}
}

func convertToUnstructured(t *testing.T, obj runtime.Object) *unstructured.Unstructured {
	t.Helper()
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatalf("Failed to convert object to unstructured: %v", err)
	}
	return &unstructured.Unstructured{Object: u}
}

func Test_extractMajorVersion(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name        string
		version     string
		expected    uint64
		expectError bool
	}{
		{
			name:        "version with v prefix and full semver",
			version:     "v3.0.0",
			expected:    3,
			expectError: false,
		},
		{
			name:        "version without v prefix",
			version:     "3.0.0",
			expected:    3,
			expectError: false,
		},
		{
			name:        "version with only major.minor",
			version:     "2.6",
			expected:    2,
			expectError: false,
		},
		{
			name:        "version with only major",
			version:     "v3",
			expected:    3,
			expectError: false,
		},
		{
			name:        "version 1.x.x",
			version:     "v1.2.3",
			expected:    1,
			expectError: false,
		},
		{
			name:        "version with pre-release tag",
			version:     "v3.0.0-rc1",
			expected:    3,
			expectError: false,
		},
		{
			name:        "version with build metadata",
			version:     "v3.0.0+20130313144700",
			expected:    3,
			expectError: false,
		},
		{
			name:        "empty version string",
			version:     "",
			expected:    0,
			expectError: true,
		},
		{
			name:        "invalid version string",
			version:     "invalid",
			expected:    0,
			expectError: true,
		},
		{
			name:        "version with non-numeric major",
			version:     "vX.0.0",
			expected:    0,
			expectError: true,
		},
		{
			name:        "just the letter v",
			version:     "v",
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractMajorVersion(tt.version)

			if tt.expectError {
				g.Expect(err).Should(HaveOccurred())
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(result).Should(Equal(tt.expected))
			}
		})
	}
}

func Test_checkServiceMeshVersionRequirement(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	t.Run("should allow deployment when ServiceMesh is not installed", func(t *testing.T) {
		kserve := createKserveCR(false)
		cli, err := fakeclient.New(fakeclient.WithObjects(kserve))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		condition := rr.Conditions.GetCondition(ServiceMeshVersionRequirement)
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))
		g.Expect(condition.Reason).Should(Equal(reasonServiceMeshNotInstalled))
		g.Expect(condition.Message).Should(ContainSubstring("not installed"))
		g.Expect(condition.Message).Should(ContainSubstring("optional"))
	})

	t.Run("should allow deployment when ServiceMesh v3 is installed", func(t *testing.T) {
		kserve := createKserveCR(false)
		serviceMeshSub := createServiceMeshSubscription()
		serviceMeshOperator := createServiceMeshOperatorCondition("v3.0.0")

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, serviceMeshSub, serviceMeshOperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		condition := rr.Conditions.GetCondition(ServiceMeshVersionRequirement)
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))
		g.Expect(condition.Reason).Should(Equal(reasonVersionCheckPassed))
		g.Expect(condition.Message).Should(ContainSubstring("v3.0.0"))
		g.Expect(condition.Message).Should(ContainSubstring("meets requirement"))
	})

	t.Run("should block deployment when ServiceMesh v2 is installed", func(t *testing.T) {
		kserve := createKserveCR(false)
		serviceMeshSub := createServiceMeshSubscription()
		serviceMeshOperator := createServiceMeshOperatorCondition("v2.6.0")

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, serviceMeshSub, serviceMeshOperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("v2.6.0"))
		g.Expect(err.Error()).Should(ContainSubstring("version 3.x"))
		g.Expect(err.Error()).Should(ContainSubstring("upgrade to ServiceMesh v3.x or uninstall"))
	})

	t.Run("should block deployment when ServiceMesh v1 is installed", func(t *testing.T) {
		kserve := createKserveCR(false)
		serviceMeshSub := createServiceMeshSubscription()
		serviceMeshOperator := createServiceMeshOperatorCondition("v1.2.3")

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, serviceMeshSub, serviceMeshOperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("v1.2.3"))
		g.Expect(err.Error()).Should(ContainSubstring("version 3.x"))
	})

	t.Run("should block deployment when ServiceMesh v4 is installed", func(t *testing.T) {
		kserve := createKserveCR(false)
		serviceMeshSub := createServiceMeshSubscription()
		serviceMeshOperator := createServiceMeshOperatorCondition("v4.0.0")

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, serviceMeshSub, serviceMeshOperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("v4.0.0"))
		g.Expect(err.Error()).Should(ContainSubstring("version 3.x"))
	})

	t.Run("should allow deployment when ServiceMesh v3.1.0 is installed", func(t *testing.T) {
		kserve := createKserveCR(false)
		serviceMeshSub := createServiceMeshSubscription()
		serviceMeshOperator := createServiceMeshOperatorCondition("v3.1.0")

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, serviceMeshSub, serviceMeshOperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		condition := rr.Conditions.GetCondition(ServiceMeshVersionRequirement)
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))
		g.Expect(condition.Reason).Should(Equal(reasonVersionCheckPassed))
	})

	t.Run("should block deployment when ServiceMesh subscription exists but operator not running", func(t *testing.T) {
		kserve := createKserveCR(false)
		serviceMeshSub := createServiceMeshSubscription()
		// No OperatorCondition created - simulates operator not running

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, serviceMeshSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("subscription found but operator is not running"))
		g.Expect(err.Error()).Should(ContainSubstring("Unable to verify version requirement"))
		g.Expect(err.Error()).Should(ContainSubstring("ensure ServiceMesh v3.x operator is running or uninstall"))
	})

	t.Run("should block deployment when version is unparseable", func(t *testing.T) {
		kserve := createKserveCR(false)
		serviceMeshSub := createServiceMeshSubscription()
		serviceMeshOperator := createServiceMeshOperatorCondition("invalid-version")

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, serviceMeshSub, serviceMeshOperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("unparseable version"))
		g.Expect(err.Error()).Should(ContainSubstring("invalid-version"))
		g.Expect(err.Error()).Should(ContainSubstring("ensure ServiceMesh v3.x is installed or uninstall"))
	})

	t.Run("should allow deployment when ServiceMesh version without v prefix is v3", func(t *testing.T) {
		kserve := createKserveCR(false)
		serviceMeshSub := createServiceMeshSubscription()
		serviceMeshOperator := createServiceMeshOperatorCondition("3.0.0")

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve, serviceMeshSub, serviceMeshOperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   kserve,
			Client:     cli,
			Conditions: conditions.NewManager(kserve, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		condition := rr.Conditions.GetCondition(ServiceMeshVersionRequirement)
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))
		g.Expect(condition.Reason).Should(Equal(reasonVersionCheckPassed))
	})

	t.Run("should return error for invalid instance type", func(t *testing.T) {
		invalidInstance := &componentApi.Dashboard{} // Wrong type
		invalidInstance.SetName("test-dashboard")
		cli, err := fakeclient.New(fakeclient.WithObjects(invalidInstance))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance:   invalidInstance,
			Client:     cli,
			Conditions: conditions.NewManager(invalidInstance, ServiceMeshVersionRequirement),
		}

		err = checkServiceMeshVersionRequirement(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("not a componentApi.Kserve"))
	})

	// Note: The following scenarios would block deployment but are difficult to test with fakeclient:
	// - API error when calling SubscriptionExists (RBAC, API server down, etc.) → BLOCKS deployment
	// - API error when calling OperatorExists (RBAC, API server down, etc.) → BLOCKS deployment
	// These scenarios require a mock client that can inject errors, which is beyond the scope
	// of these unit tests. They should be covered by integration tests with real cluster conditions.
}

// createServiceMeshSubscription creates a ServiceMesh subscription for testing.
func createServiceMeshSubscription() *v1alpha1.Subscription {
	return &v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceMeshOperatorSubscription,
			Namespace: testOperatorNamespace,
		},
		Spec: &v1alpha1.SubscriptionSpec{
			Package: serviceMeshOperatorSubscription,
		},
	}
}

// createServiceMeshOperatorCondition creates an OperatorCondition with the specified version.
func createServiceMeshOperatorCondition(version string) *unstructured.Unstructured {
	operatorCondition := &unstructured.Unstructured{}
	operatorCondition.SetGroupVersionKind(gvk.OperatorCondition)
	operatorCondition.SetName(serviceMeshOperatorPrefix + "." + version)

	return operatorCondition
}
