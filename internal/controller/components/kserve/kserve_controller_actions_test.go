//nolint:testpackage
package kserve

import (
	"encoding/json"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestCustomizeKserveConfigMap(t *testing.T) { //nolint:maintidx
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
		var ingressData map[string]any
		err = json.Unmarshal([]byte(updatedConfigMap.Data[IngressConfigKeyName]), &ingressData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ingressData["disableIngressCreation"]).Should(BeTrue())

		// verify service is configured as headless (default)
		var serviceData map[string]any
		err = json.Unmarshal([]byte(updatedConfigMap.Data[ServiceConfigKeyName]), &serviceData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(serviceData["serviceClusterIPNone"]).Should(BeTrue())

		// verify localModel jobNamespace and enabled
		var localModelData map[string]interface{}
		err = json.Unmarshal([]byte(updatedConfigMap.Data[LocalModelConfigKeyName]), &localModelData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(localModelData["jobNamespace"]).Should(Equal(cluster.GetApplicationNamespace()))
		g.Expect(localModelData["enabled"]).Should(BeFalse())
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

		var ingressData map[string]any
		err = json.Unmarshal([]byte(updatedConfigMap.Data[IngressConfigKeyName]), &ingressData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ingressData["disableIngressCreation"]).Should(BeTrue())

		var serviceData map[string]any
		err = json.Unmarshal([]byte(updatedConfigMap.Data[ServiceConfigKeyName]), &serviceData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(serviceData["serviceClusterIPNone"]).Should(BeFalse())

		// verify localModel jobNamespace and enabled
		var localModelData map[string]interface{}
		err = json.Unmarshal([]byte(updatedConfigMap.Data[LocalModelConfigKeyName]), &localModelData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(localModelData["jobNamespace"]).Should(Equal(cluster.GetApplicationNamespace()))
		g.Expect(localModelData["enabled"]).Should(BeFalse())
	})

	t.Run("Test localModel enabled when ModelCache is Managed", func(t *testing.T) {
		cacheSize := resource.MustParse("100Gi")
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &cacheSize,
						NodeNames:       []string{"node1"},
					},
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

		var localModelData map[string]interface{}
		err = json.Unmarshal([]byte(updatedConfigMap.Data[LocalModelConfigKeyName]), &localModelData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(localModelData["enabled"]).Should(BeTrue())
		g.Expect(localModelData["jobNamespace"]).Should(Equal(cluster.GetApplicationNamespace()))
	})

	t.Run("Test error when localModel key is absent", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{},
			},
		}

		cm := &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
			ObjectMeta: metav1.ObjectMeta{Name: kserveConfigMapName},
			Data: map[string]string{
				IngressConfigKeyName: `{"disableIngressCreation": false}`,
				ServiceConfigKeyName: `{"serviceClusterIPNone": false}`,
			},
		}
		initialDeployment := createTestDeployment()
		resources := []unstructured.Unstructured{
			*convertToUnstructured(t, cm),
			*convertToUnstructured(t, initialDeployment),
		}

		rr := &odhtypes.ReconciliationRequest{
			Instance:  kserve,
			Resources: resources,
		}

		err := customizeKserveConfigMap(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring(LocalModelConfigKeyName))
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
			if resource.GetKind() == "Deployment" && resource.GetName() == isvcControllerDeployment {
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

	t.Run("Test KServe skips hash annotation when deployment is missing (e.g., XKS)", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
		}

		// create reconciliation request with ConfigMap but without deployment (simulates XKS manifests)
		initialConfigMap := createTestConfigMap()
		resources := []unstructured.Unstructured{
			*convertToUnstructured(t, initialConfigMap),
		}

		rr := &odhtypes.ReconciliationRequest{
			Instance:  kserve,
			Resources: resources,
		}

		// Should not error - ConfigMap is updated, but deployment hash annotation is skipped
		err := customizeKserveConfigMap(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})
}

func TestCustomizeKserveConfigMapOAuthProxy(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	t.Run("Test KServe config: oauthProxy resource overrides", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					OAuthProxy: &componentApi.OAuthProxyConfig{
						Resources: &componentApi.OAuthProxyResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("256Mi"),
								corev1.ResourceCPU:    resource.MustParse("200m"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("512Mi"),
								corev1.ResourceCPU:    resource.MustParse("500m"),
							},
						},
					},
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

		var oauthProxyData map[string]any
		err = json.Unmarshal([]byte(updatedConfigMap.Data[OAuthProxyConfigKeyName]), &oauthProxyData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(oauthProxyData["image"]).Should(Equal("registry.example.com/oauth-proxy:latest"))
		g.Expect(oauthProxyData["memoryRequest"]).Should(Equal("256Mi"))
		g.Expect(oauthProxyData["memoryLimit"]).Should(Equal("512Mi"))
		g.Expect(oauthProxyData["cpuRequest"]).Should(Equal("200m"))
		g.Expect(oauthProxyData["cpuLimit"]).Should(Equal("500m"))
	})

	t.Run("Test KServe config: oauthProxy partial overrides preserve defaults", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					OAuthProxy: &componentApi.OAuthProxyConfig{
						Resources: &componentApi.OAuthProxyResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					},
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

		var oauthProxyData map[string]any
		err = json.Unmarshal([]byte(updatedConfigMap.Data[OAuthProxyConfigKeyName]), &oauthProxyData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(oauthProxyData["image"]).Should(Equal("registry.example.com/oauth-proxy:latest"))
		g.Expect(oauthProxyData["memoryRequest"]).Should(Equal("64Mi"))
		g.Expect(oauthProxyData["memoryLimit"]).Should(Equal("512Mi"))
		g.Expect(oauthProxyData["cpuRequest"]).Should(Equal("100m"))
		g.Expect(oauthProxyData["cpuLimit"]).Should(Equal("200m"))
	})

	t.Run("Test KServe config: nil oauthProxy preserves all defaults", func(t *testing.T) {
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

		var oauthProxyData map[string]any
		err = json.Unmarshal([]byte(updatedConfigMap.Data[OAuthProxyConfigKeyName]), &oauthProxyData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(oauthProxyData["image"]).Should(Equal("registry.example.com/oauth-proxy:latest"))
		g.Expect(oauthProxyData["memoryRequest"]).Should(Equal("64Mi"))
		g.Expect(oauthProxyData["memoryLimit"]).Should(Equal("128Mi"))
		g.Expect(oauthProxyData["cpuRequest"]).Should(Equal("100m"))
		g.Expect(oauthProxyData["cpuLimit"]).Should(Equal("200m"))
	})

	t.Run("Test KServe config: non-nil empty oauthProxy preserves all defaults", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					OAuthProxy: &componentApi.OAuthProxyConfig{},
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

		g.Expect(updatedConfigMap.Data).Should(HaveKey(OAuthProxyConfigKeyName))
		var oauthProxyData map[string]any
		err = json.Unmarshal([]byte(updatedConfigMap.Data[OAuthProxyConfigKeyName]), &oauthProxyData)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(oauthProxyData["image"]).Should(Equal("registry.example.com/oauth-proxy:latest"))
		g.Expect(oauthProxyData["memoryRequest"]).Should(Equal("64Mi"))
		g.Expect(oauthProxyData["memoryLimit"]).Should(Equal("128Mi"))
		g.Expect(oauthProxyData["cpuRequest"]).Should(Equal("100m"))
		g.Expect(oauthProxyData["cpuLimit"]).Should(Equal("200m"))
	})

	t.Run("Test KServe config: missing oauthProxy key in ConfigMap is tolerated", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					OAuthProxy: &componentApi.OAuthProxyConfig{
						Resources: &componentApi.OAuthProxyResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					},
				},
			},
		}

		cmWithoutOAuthProxy := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: kserveConfigMapName,
			},
			Data: map[string]string{
				IngressConfigKeyName:    `{"disableIngressCreation": false}`,
				ServiceConfigKeyName:    `{"serviceClusterIPNone": false}`,
				LocalModelConfigKeyName: `{"jobNamespace": "default", "enabled": false}`,
			},
		}

		initialDeployment := createTestDeployment()
		resources := []unstructured.Unstructured{
			*convertToUnstructured(t, cmWithoutOAuthProxy),
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
		g.Expect(updatedConfigMap.Data).ShouldNot(HaveKey(OAuthProxyConfigKeyName))
	})
}

//nolint:maintidx
func TestCheckSubscriptionDependencies(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	const happyCondition = "Ready"

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	t.Run("RHCL and cert-manager subscriptions absent sets LLMInferenceServiceDependencies to False", func(t *testing.T) {
		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition,
			LLMInferenceServiceDependencies, LLMInferenceServiceWideEPDependencies)

		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkSubscriptionDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionFalse))
		g.Expect(got.Message).Should(ContainSubstring("Red Hat Connectivity Link"))
		g.Expect(got.Message).Should(ContainSubstring("cert-manager operator"))
	})

	t.Run("RHCL and cert-manager subscriptions present sets LLMInferenceServiceDependencies to True", func(t *testing.T) {
		rhclSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rhclOperatorSubscription,
				Namespace: "openshift-operators",
			},
		}
		certManagerSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certManagerOperatorSubscription,
				Namespace: "cert-manager-operator",
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(rhclSub, certManagerSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition,
			LLMInferenceServiceDependencies, LLMInferenceServiceWideEPDependencies)

		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkSubscriptionDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionTrue))
	})

	//nolint:dupl
	t.Run("Only RHCL absent sets LLMInferenceServiceDependencies to False", func(t *testing.T) {
		certManagerSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certManagerOperatorSubscription,
				Namespace: "cert-manager-operator",
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(certManagerSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition,
			LLMInferenceServiceDependencies, LLMInferenceServiceWideEPDependencies)

		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkSubscriptionDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionFalse))
		g.Expect(got.Message).Should(ContainSubstring("Red Hat Connectivity Link"))
		g.Expect(got.Message).ShouldNot(ContainSubstring("cert-manager operator"))
	})

	//nolint:dupl
	t.Run("Only cert-manager absent sets LLMInferenceServiceDependencies to False", func(t *testing.T) {
		rhclSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rhclOperatorSubscription,
				Namespace: "openshift-operators",
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(rhclSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition,
			LLMInferenceServiceDependencies, LLMInferenceServiceWideEPDependencies)

		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkSubscriptionDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionFalse))
		g.Expect(got.Message).Should(ContainSubstring("cert-manager operator"))
		g.Expect(got.Message).ShouldNot(ContainSubstring("Red Hat Connectivity Link"))
	})

	t.Run("Only LWS absent sets LLMInferenceServiceWideEPDependencies to False", func(t *testing.T) {
		rhclSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rhclOperatorSubscription,
				Namespace: "openshift-operators",
			},
		}
		certManagerSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certManagerOperatorSubscription,
				Namespace: "cert-manager-operator",
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(rhclSub, certManagerSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition,
			LLMInferenceServiceDependencies, LLMInferenceServiceWideEPDependencies)

		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkSubscriptionDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// RHCL and cert-manager are present, so LLMInferenceServiceDependencies should be True
		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionTrue))

		// LWS is absent, so LLMInferenceServiceWideEPDependencies should be False
		gotWide := condManager.GetCondition(LLMInferenceServiceWideEPDependencies)
		g.Expect(gotWide).ShouldNot(BeNil())
		g.Expect(gotWide.Status).Should(Equal(metav1.ConditionFalse))
		g.Expect(gotWide.Message).Should(ContainSubstring("LeaderWorkerSet"))
		g.Expect(gotWide.Message).ShouldNot(ContainSubstring("Red Hat Connectivity Link"))
		g.Expect(gotWide.Message).ShouldNot(ContainSubstring("cert-manager operator"))
	})

	t.Run("All subscriptions (RHCL, LWS, cert-manager) present sets both conditions to True", func(t *testing.T) {
		rhclSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rhclOperatorSubscription,
				Namespace: "openshift-operators",
			},
		}
		lwsSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lwsOperatorSubscription,
				Namespace: "openshift-operators",
			},
		}
		certManagerSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certManagerOperatorSubscription,
				Namespace: "cert-manager-operator",
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(rhclSub, lwsSub, certManagerSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition,
			LLMInferenceServiceDependencies, LLMInferenceServiceWideEPDependencies)

		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkSubscriptionDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionTrue))

		gotWide := condManager.GetCondition(LLMInferenceServiceWideEPDependencies)
		g.Expect(gotWide).ShouldNot(BeNil())
		g.Expect(gotWide.Status).Should(Equal(metav1.ConditionTrue))
	})

	t.Run("Only cert-manager absent with all others present sets LLMInferenceServiceWideEPDependencies to False", func(t *testing.T) {
		rhclSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rhclOperatorSubscription,
				Namespace: "openshift-operators",
			},
		}
		lwsSub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lwsOperatorSubscription,
				Namespace: "openshift-operators",
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(rhclSub, lwsSub))
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition,
			LLMInferenceServiceDependencies, LLMInferenceServiceWideEPDependencies)

		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkSubscriptionDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// LLMInferenceServiceDependencies should also be False (cert-manager missing)
		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionFalse))
		g.Expect(got.Message).Should(ContainSubstring("cert-manager operator"))
		g.Expect(got.Message).ShouldNot(ContainSubstring("Red Hat Connectivity Link"))

		// LLMInferenceServiceWideEPDependencies should be False (cert-manager missing)
		gotWide := condManager.GetCondition(LLMInferenceServiceWideEPDependencies)
		g.Expect(gotWide).ShouldNot(BeNil())
		g.Expect(gotWide.Status).Should(Equal(metav1.ConditionFalse))
		g.Expect(gotWide.Message).Should(ContainSubstring("cert-manager operator"))
		g.Expect(gotWide.Message).ShouldNot(ContainSubstring("Red Hat Connectivity Link"))
		g.Expect(gotWide.Message).ShouldNot(ContainSubstring("LeaderWorkerSet"))
	})

	t.Run("Non-OpenShift cluster skips checks and don't add conditions", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift}) })

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition)

		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		// No subscriptions present, but cluster is Kubernetes so checks should be skipped
		action := checkSubscriptionDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).Should(BeNil())

		gotWide := condManager.GetCondition(LLMInferenceServiceWideEPDependencies)
		g.Expect(gotWide).Should(BeNil())
	})
}

func TestSortLLMInferenceServiceConfigLast(t *testing.T) {
	newRes := func(group, version, kind, name string) unstructured.Unstructured {
		u := unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
		u.SetName(name)
		return u
	}

	llmISvcConfig := func(name string) unstructured.Unstructured {
		return newRes(
			gvk.LLMInferenceServiceConfigV1Alpha2.Group,
			gvk.LLMInferenceServiceConfigV1Alpha2.Version,
			gvk.LLMInferenceServiceConfigV1Alpha2.Kind,
			name,
		)
	}

	t.Run("LLMInferenceServiceConfig resources are placed after all other resources", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		input := []unstructured.Unstructured{
			llmISvcConfig("config-a"),
			newRes("apps", "v1", "Deployment", "my-deploy"),
			llmISvcConfig("config-b"),
			newRes("", "v1", "Service", "my-svc"),
		}

		result, err := sortLLMInferenceServiceConfigLast(ctx, input)
		g.Expect(err).NotTo(HaveOccurred())

		// Non-LLMInferenceServiceConfig resources should come first
		g.Expect(result[len(result)-2].GetName()).To(Equal("config-a"))
		g.Expect(result[len(result)-1].GetName()).To(Equal("config-b"))

		// All non-LLMInferenceServiceConfig resources should precede them
		for _, r := range result[:len(result)-2] {
			g.Expect(r.GetKind()).NotTo(Equal(gvk.LLMInferenceServiceConfigV1Alpha2.Kind))
		}
	})

	t.Run("preserves relative order among LLMInferenceServiceConfig resources", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		input := []unstructured.Unstructured{
			llmISvcConfig("config-z"),
			newRes("apps", "v1", "Deployment", "deploy"),
			llmISvcConfig("config-a"),
			llmISvcConfig("config-m"),
		}

		result, err := sortLLMInferenceServiceConfigLast(ctx, input)
		g.Expect(err).NotTo(HaveOccurred())

		// The three LLMInferenceServiceConfig resources should be at the end,
		// in their original relative order (stable sort).
		configs := result[len(result)-3:]
		g.Expect(configs[0].GetName()).To(Equal("config-z"))
		g.Expect(configs[1].GetName()).To(Equal("config-a"))
		g.Expect(configs[2].GetName()).To(Equal("config-m"))
	})

	t.Run("no LLMInferenceServiceConfig resources leaves order unchanged", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		input := []unstructured.Unstructured{
			newRes("apps", "v1", "Deployment", "deploy"),
			newRes("", "v1", "Service", "svc"),
			newRes("", "v1", "ConfigMap", "cm"),
		}

		result, err := sortLLMInferenceServiceConfigLast(ctx, input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(3))
		g.Expect(result[0].GetName()).To(Equal("deploy"))
		g.Expect(result[1].GetName()).To(Equal("svc"))
		g.Expect(result[2].GetName()).To(Equal("cm"))
	})

	t.Run("all LLMInferenceServiceConfig resources preserves input order", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		input := []unstructured.Unstructured{
			llmISvcConfig("config-b"),
			llmISvcConfig("config-a"),
			llmISvcConfig("config-c"),
		}

		result, err := sortLLMInferenceServiceConfigLast(ctx, input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(3))
		g.Expect(result[0].GetName()).To(Equal("config-b"))
		g.Expect(result[1].GetName()).To(Equal("config-a"))
		g.Expect(result[2].GetName()).To(Equal("config-c"))
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		result, err := sortLLMInferenceServiceConfigLast(ctx, nil)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(BeEmpty())
	})
}

func TestDeleteLLMInferenceServiceConfigs(t *testing.T) {
	t.Run("should delete owned LLMInferenceServiceConfig resources and requeue", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
				UID:  "test-kserve-uid",
			},
		}

		llmConfig1 := newLLMInferenceServiceConfig("config-1", "test-kserve-uid")
		llmConfig2 := newLLMInferenceServiceConfig("config-2", "test-kserve-uid")

		cli := buildClientWithLLMConfigs(t, llmConfig1, llmConfig2)

		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: instance,
		}

		err := deleteLLMInferenceServiceConfigs(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("waiting for 2 LLMInferenceServiceConfig"))

		err = deleteLLMInferenceServiceConfigs(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should not delete resources not owned by this instance", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
				UID:  "test-kserve-uid",
			},
		}

		llmConfig := newLLMInferenceServiceConfig("config-1", "different-uid")

		cli := buildClientWithLLMConfigs(t, llmConfig)

		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: instance,
		}

		err := deleteLLMInferenceServiceConfigs(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha2)
		g.Expect(cli.List(ctx, list)).Should(Succeed())
		g.Expect(list.Items).Should(HaveLen(1))
	})

	t.Run("should succeed when no resources exist", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
				UID:  "test-kserve-uid",
			},
		}

		cli := buildClientWithLLMConfigs(t)

		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: instance,
		}

		err := deleteLLMInferenceServiceConfigs(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should succeed when CRD does not exist", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
				UID:  "test-kserve-uid",
			},
		}

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: instance,
		}

		err = deleteLLMInferenceServiceConfigs(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should only delete owned resources among mixed ownership", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
				UID:  "my-uid",
			},
		}

		ownedConfig := newLLMInferenceServiceConfig("owned-config", "my-uid")
		unownedConfig := newLLMInferenceServiceConfig("unowned-config", "other-uid")

		cli := buildClientWithLLMConfigs(t, ownedConfig, unownedConfig)

		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: instance,
		}

		err := deleteLLMInferenceServiceConfigs(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("waiting for 1 LLMInferenceServiceConfig"))

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha2)
		g.Expect(cli.List(ctx, list)).Should(Succeed())
		g.Expect(list.Items).Should(HaveLen(1))
		g.Expect(list.Items[0].GetName()).Should(Equal("unowned-config"))
	})
}

func newLLMInferenceServiceConfig(name, ownerUID string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": gvk.LLMInferenceServiceConfigV1Alpha2.Group + "/" + gvk.LLMInferenceServiceConfigV1Alpha2.Version,
			"kind":       gvk.LLMInferenceServiceConfigV1Alpha2.Kind,
			"metadata": map[string]any{
				"name":      name,
				"namespace": "test-ns",
				"ownerReferences": []any{
					map[string]any{
						"apiVersion": componentApi.GroupVersion.String(),
						"kind":       componentApi.KserveKind,
						"name":       componentApi.KserveInstanceName,
						"uid":        ownerUID,
					},
				},
			},
		},
	}
}

func buildClientWithLLMConfigs(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()

	s, err := testscheme.New()
	if err != nil {
		t.Fatalf("failed to create scheme: %v", err)
	}

	fakeMapper := meta.NewDefaultRESTMapper(s.PreferredVersionAllGroups())
	for kt := range s.AllKnownTypes() {
		fakeMapper.Add(kt, meta.RESTScopeNamespace)
	}

	fakeMapper.Add(gvk.LLMInferenceServiceConfigV1Alpha1, meta.RESTScopeNamespace)
	fakeMapper.Add(gvk.LLMInferenceServiceConfigV1Alpha2, meta.RESTScopeNamespace)

	return clientFake.NewClientBuilder().
		WithScheme(s).
		WithRESTMapper(fakeMapper).
		WithObjects(objs...).
		Build()
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
			LocalModelConfigKeyName: `{
				"jobNamespace": "wrong-namespace",
				"enabled": true
			}`,
			OAuthProxyConfigKeyName: `{
				"image": "registry.example.com/oauth-proxy:latest",
				"memoryRequest": "64Mi",
				"memoryLimit": "128Mi",
				"cpuRequest": "100m",
				"cpuLimit": "200m"
			}`,
		},
	}
}

func createTestDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: isvcControllerDeployment,
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

func TestInitialize(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	t.Run("uses default overlay when ModelCache is nil", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Instance: kserve}

		err := initialize(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Manifests).Should(HaveLen(2))
		g.Expect(rr.Manifests[0].SourcePath).Should(Equal(kserveManifestSourcePath))
		g.Expect(rr.Manifests[1].ContextDir).Should(Equal("connectionAPI"))
	})

	t.Run("uses default overlay when ModelCache is Removed", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Instance: kserve}

		err := initialize(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Manifests).Should(HaveLen(2))
		g.Expect(rr.Manifests[0].SourcePath).Should(Equal(kserveManifestSourcePath))
	})

	t.Run("uses base overlay plus modelcache overlay when ModelCache is Managed", func(t *testing.T) {
		kserve := &componentApi.Kserve{
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						NodeNames:       []string{"node1"},
					},
				},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Instance: kserve}

		err := initialize(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Manifests).Should(HaveLen(3))
		g.Expect(rr.Manifests[0].SourcePath).Should(Equal(kserveManifestSourcePath))
		g.Expect(rr.Manifests[1].ContextDir).Should(Equal("connectionAPI"))
		g.Expect(rr.Manifests[2].SourcePath).Should(Equal(kserveManifestSourcePathModelCache))
	})

	t.Run("uses XKS overlay plus modelcache overlay when ModelCache is Managed on Kubernetes", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		kserve := &componentApi.Kserve{
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						NodeNames:       []string{"node1"},
					},
				},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Instance: kserve}

		err := initialize(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Manifests).Should(HaveLen(3))
		g.Expect(rr.Manifests[0].SourcePath).Should(Equal(kserveManifestSourcePathXKS))
		g.Expect(rr.Manifests[1].ContextDir).Should(Equal("connectionAPI"))
		g.Expect(rr.Manifests[2].SourcePath).Should(Equal(kserveManifestSourcePathModelCache))
	})
}

func TestCreateModelCachePVAndPVC(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	t.Run("creates PV and PVC when ModelCache is Managed", func(t *testing.T) {
		cacheSize := resource.MustParse("200Gi")
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &cacheSize,
						NodeNames:       []string{"node1"},
					},
				},
			},
		}
		kserve.SetGroupVersionKind(componentApi.GroupVersion.WithKind(componentApi.KserveKind))

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance: kserve,
			Client:   cli,
		}

		err = createModelCachePVAndPVC(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify PV
		pv := &corev1.PersistentVolume{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "kserve-localmodelnode-pv"}, pv)).Should(Succeed())
		g.Expect(pv.Spec.Capacity[corev1.ResourceStorage]).Should(Equal(cacheSize))
		g.Expect(pv.Spec.StorageClassName).Should(Equal("local-storage"))
		g.Expect(pv.Spec.HostPath).ShouldNot(BeNil())
		g.Expect(pv.Spec.HostPath.Path).Should(Equal("/var/lib/kserve/models"))
		g.Expect(pv.OwnerReferences).Should(HaveLen(1))
		g.Expect(pv.OwnerReferences[0].Name).Should(Equal(componentApi.KserveInstanceName))

		// Verify PVC
		pvc := &corev1.PersistentVolumeClaim{}
		g.Expect(cli.Get(ctx, client.ObjectKey{
			Name:      "kserve-localmodelnode-pvc",
			Namespace: cluster.GetApplicationNamespace(),
		}, pvc)).Should(Succeed())
		g.Expect(pvc.Spec.Resources.Requests[corev1.ResourceStorage]).Should(Equal(cacheSize))
		g.Expect(pvc.Spec.VolumeName).Should(Equal("kserve-localmodelnode-pv"))
		g.Expect(*pvc.Spec.StorageClassName).Should(Equal("local-storage"))
		g.Expect(pvc.OwnerReferences).Should(HaveLen(1))
		g.Expect(pvc.OwnerReferences[0].Name).Should(Equal(componentApi.KserveInstanceName))
	})

	t.Run("updates PV and PVC when cacheSize changes", func(t *testing.T) {
		initialSize := resource.MustParse("100Gi")
		updatedSize := resource.MustParse("500Gi")

		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &initialSize,
						NodeNames:       []string{"node1"},
					},
				},
			},
		}
		kserve.SetGroupVersionKind(componentApi.GroupVersion.WithKind(componentApi.KserveKind))

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Instance: kserve,
			Client:   cli,
		}

		// First create
		err = createModelCachePVAndPVC(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Update cacheSize
		kserve.Spec.ModelCache.CacheSize = &updatedSize
		err = createModelCachePVAndPVC(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify PV updated
		pv := &corev1.PersistentVolume{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "kserve-localmodelnode-pv"}, pv)).Should(Succeed())
		g.Expect(pv.Spec.Capacity[corev1.ResourceStorage]).Should(Equal(updatedSize))

		// Verify PVC updated
		pvc := &corev1.PersistentVolumeClaim{}
		g.Expect(cli.Get(ctx, client.ObjectKey{
			Name:      "kserve-localmodelnode-pvc",
			Namespace: cluster.GetApplicationNamespace(),
		}, pvc)).Should(Succeed())
		g.Expect(pvc.Spec.Resources.Requests[corev1.ResourceStorage]).Should(Equal(updatedSize))
	})
}

func TestLabelModelCacheNodes(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	t.Run("labels nodes by NodeNames", func(t *testing.T) {
		node1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node1"},
		}
		node2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node2"},
		}
		node3 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node3"},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(node1, node2, node3))
		g.Expect(err).ShouldNot(HaveOccurred())

		cacheSize := resource.MustParse("100Gi")
		kserve := &componentApi.Kserve{
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &cacheSize,
						NodeNames:       []string{"node1", "node2"},
					},
				},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Instance: kserve, Client: cli}

		err = labelModelCacheNodes(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		for _, name := range []string{"node1", "node2"} {
			updated := &corev1.Node{}
			g.Expect(cli.Get(ctx, client.ObjectKey{Name: name}, updated)).Should(Succeed())
			g.Expect(updated.Labels["kserve/localmodel"]).Should(Equal("worker"))
		}

		// node3 should NOT be labeled since it's not in NodeNames
		unlabeled := &corev1.Node{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "node3"}, unlabeled)).Should(Succeed())
		g.Expect(unlabeled.Labels).ShouldNot(HaveKey("kserve/localmodel"))
	})

	t.Run("skips nodes already labeled", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node1",
				Labels: map[string]string{"kserve/localmodel": "worker"},
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(node))
		g.Expect(err).ShouldNot(HaveOccurred())

		cacheSize := resource.MustParse("100Gi")
		kserve := &componentApi.Kserve{
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &cacheSize,
						NodeNames:       []string{"node1"},
					},
				},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Instance: kserve, Client: cli}

		err = labelModelCacheNodes(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		updated := &corev1.Node{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "node1"}, updated)).Should(Succeed())
		g.Expect(updated.Labels["kserve/localmodel"]).Should(Equal("worker"))
	})

	t.Run("labels nodes by NodeSelector", func(t *testing.T) {
		gpuNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "gpu-node",
				Labels: map[string]string{"nvidia.com/gpu": "true"},
			},
		}
		cpuNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "cpu-node",
				Labels: map[string]string{"node-type": "cpu"},
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(gpuNode, cpuNode))
		g.Expect(err).ShouldNot(HaveOccurred())

		cacheSize := resource.MustParse("100Gi")
		kserve := &componentApi.Kserve{
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &cacheSize,
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"nvidia.com/gpu": "true"},
						},
					},
				},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Instance: kserve, Client: cli}

		err = labelModelCacheNodes(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// GPU node should be labeled
		updated := &corev1.Node{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "gpu-node"}, updated)).Should(Succeed())
		g.Expect(updated.Labels["kserve/localmodel"]).Should(Equal("worker"))

		// CPU node should NOT be labeled
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "cpu-node"}, updated)).Should(Succeed())
		g.Expect(updated.Labels).ShouldNot(HaveKey("kserve/localmodel"))
	})

	t.Run("removes stale labels from nodes no longer in desired set", func(t *testing.T) {
		desiredNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node1",
				Labels: map[string]string{modelCacheLabelKey: modelCacheLabelValue},
			},
		}
		staleNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-removed",
				Labels: map[string]string{modelCacheLabelKey: modelCacheLabelValue},
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(desiredNode, staleNode))
		g.Expect(err).ShouldNot(HaveOccurred())

		cacheSize := resource.MustParse("100Gi")
		kserveObj := &componentApi.Kserve{
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &cacheSize,
						NodeNames:       []string{"node1"},
					},
				},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Instance: kserveObj, Client: cli}

		err = labelModelCacheNodes(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		updated := &corev1.Node{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "node1"}, updated)).Should(Succeed())
		g.Expect(updated.Labels[modelCacheLabelKey]).Should(Equal(modelCacheLabelValue))

		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "node-removed"}, updated)).Should(Succeed())
		g.Expect(updated.Labels).ShouldNot(HaveKey(modelCacheLabelKey))
	})
}

func TestCreateLocalModelNodeGroup(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	t.Run("creates LocalModelNodeGroup when Managed", func(t *testing.T) {
		cacheSize := resource.MustParse("200Gi")
		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &cacheSize,
						NodeNames:       []string{"node1"},
					},
				},
			},
		}
		kserve.SetGroupVersionKind(componentApi.GroupVersion.WithKind(componentApi.KserveKind))

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Instance: kserve, Client: cli}

		err = createLocalModelNodeGroup(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk.LocalModelNodeGroup)
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "workers"}, obj)).Should(Succeed())

		spec, ok := obj.Object["spec"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		g.Expect(spec["storageLimit"]).Should(Equal("200Gi"))

		pvSpec, ok := spec["persistentVolumeSpec"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		capacity, ok := pvSpec["capacity"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		g.Expect(capacity["storage"]).Should(Equal("200Gi"))

		pvcSpec, ok := spec["persistentVolumeClaimSpec"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		pvcResources, ok := pvcSpec["resources"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		requests, ok := pvcResources["requests"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		g.Expect(requests["storage"]).Should(Equal("200Gi"))

		ownerRefs := obj.GetOwnerReferences()
		g.Expect(ownerRefs).Should(HaveLen(1))
		g.Expect(ownerRefs[0].Name).Should(Equal(componentApi.KserveInstanceName))
	})

	t.Run("updates LocalModelNodeGroup when cacheSize changes", func(t *testing.T) {
		initialSize := resource.MustParse("100Gi")
		updatedSize := resource.MustParse("500Gi")

		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &initialSize,
						NodeNames:       []string{"node1"},
					},
				},
			},
		}
		kserve.SetGroupVersionKind(componentApi.GroupVersion.WithKind(componentApi.KserveKind))

		cli, err := fakeclient.New(fakeclient.WithObjects(kserve))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Instance: kserve, Client: cli}

		// First create
		err = createLocalModelNodeGroup(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Update cacheSize
		kserve.Spec.ModelCache.CacheSize = &updatedSize
		err = createLocalModelNodeGroup(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify updated values
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk.LocalModelNodeGroup)
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "workers"}, obj)).Should(Succeed())

		spec, ok := obj.Object["spec"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		g.Expect(spec["storageLimit"]).Should(Equal("500Gi"))

		pvSpec, ok := spec["persistentVolumeSpec"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		capacity, ok := pvSpec["capacity"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		g.Expect(capacity["storage"]).Should(Equal("500Gi"))

		pvcSpec, ok := spec["persistentVolumeClaimSpec"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		pvcResources, ok := pvcSpec["resources"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		requests, ok := pvcResources["requests"].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		g.Expect(requests["storage"]).Should(Equal("500Gi"))
	})
}

func convertToUnstructured(t *testing.T, obj runtime.Object) *unstructured.Unstructured {
	t.Helper()
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatalf("Failed to convert object to unstructured: %v", err)
	}
	return &unstructured.Unstructured{Object: u}
}

func TestForceReconcileKserveAgentImage(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	const expectedImage = "registry.example.com/kserve-agent:v1.0"

	newConfigMapWithOpenshiftConfig := func(image string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kserveConfigMapName,
				Namespace: cluster.GetApplicationNamespace(),
			},
			Data: map[string]string{
				OpenshiftConfigKeyName: `{"modelcachePermissionFixImage": "` + image + `"}`,
			},
		}
	}

	t.Run("corrects drift when image has been modified", func(t *testing.T) {
		t.Setenv("RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE", expectedImage)

		cm := newConfigMapWithOpenshiftConfig("wrong-image:latest")
		cli, err := fakeclient.New(fakeclient.WithObjects(cm))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}
		err = forceReconcileKserveAgentImage(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		updated := &corev1.ConfigMap{}
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(cm), updated)).Should(Succeed())

		var openshiftConfig map[string]any
		err = json.Unmarshal([]byte(updated.Data[OpenshiftConfigKeyName]), &openshiftConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(openshiftConfig["modelcachePermissionFixImage"]).Should(Equal(expectedImage))
	})

	t.Run("no-op when image already matches", func(t *testing.T) {
		t.Setenv("RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE", expectedImage)

		cm := newConfigMapWithOpenshiftConfig(expectedImage)
		cli, err := fakeclient.New(fakeclient.WithObjects(cm))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}
		err = forceReconcileKserveAgentImage(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("no-op when ConfigMap does not exist", func(t *testing.T) {
		t.Setenv("RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE", expectedImage)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}
		err = forceReconcileKserveAgentImage(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("no-op when openshiftConfig key is absent", func(t *testing.T) {
		t.Setenv("RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE", expectedImage)

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kserveConfigMapName,
				Namespace: cluster.GetApplicationNamespace(),
			},
			Data: map[string]string{
				"someOtherKey": `{"foo": "bar"}`,
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(cm))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}
		err = forceReconcileKserveAgentImage(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("no-op when env var is not set", func(t *testing.T) {
		cm := newConfigMapWithOpenshiftConfig("some-image:latest")
		cli, err := fakeclient.New(fakeclient.WithObjects(cm))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}
		err = forceReconcileKserveAgentImage(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify image was NOT changed
		updated := &corev1.ConfigMap{}
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(cm), updated)).Should(Succeed())

		var openshiftConfig map[string]any
		err = json.Unmarshal([]byte(updated.Data[OpenshiftConfigKeyName]), &openshiftConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(openshiftConfig["modelcachePermissionFixImage"]).Should(Equal("some-image:latest"))
	})

	t.Run("preserves other fields in openshiftConfig", func(t *testing.T) {
		t.Setenv("RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE", expectedImage)

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kserveConfigMapName,
				Namespace: cluster.GetApplicationNamespace(),
			},
			Data: map[string]string{
				OpenshiftConfigKeyName: `{"modelcachePermissionFixImage": "wrong-image", "otherField": "preserve-me"}`,
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(cm))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}
		err = forceReconcileKserveAgentImage(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		updated := &corev1.ConfigMap{}
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(cm), updated)).Should(Succeed())

		var openshiftConfig map[string]any
		err = json.Unmarshal([]byte(updated.Data[OpenshiftConfigKeyName]), &openshiftConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(openshiftConfig["modelcachePermissionFixImage"]).Should(Equal(expectedImage))
		g.Expect(openshiftConfig["otherField"]).Should(Equal("preserve-me"))
	})
}

func TestUpdateNamespacePSA(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	newNamespace := func(name, psaLevel string) *corev1.Namespace {
		return &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					labels.SecurityEnforce: psaLevel,
				},
			},
		}
	}

	t.Run("upgrades baseline to privileged and sets annotation", func(t *testing.T) {
		ns := newNamespace(cluster.GetApplicationNamespace(), "baseline")
		cli, err := fakeclient.New(fakeclient.WithObjects(ns))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = updateNamespacePSA(ctx, cli, "privileged")
		g.Expect(err).ShouldNot(HaveOccurred())

		updated := &corev1.Namespace{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cluster.GetApplicationNamespace()}, updated)).Should(Succeed())
		g.Expect(updated.Labels[labels.SecurityEnforce]).To(Equal("privileged"))
		g.Expect(updated.Annotations[annotations.PSAElevatedBy]).To(Equal("kserve-modelcache"))
	})

	t.Run("restores privileged to baseline and removes annotation", func(t *testing.T) {
		ns := newNamespace(cluster.GetApplicationNamespace(), "privileged")
		ns.Annotations = map[string]string{annotations.PSAElevatedBy: "kserve-modelcache"}
		cli, err := fakeclient.New(fakeclient.WithObjects(ns))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = updateNamespacePSA(ctx, cli, "baseline")
		g.Expect(err).ShouldNot(HaveOccurred())

		updated := &corev1.Namespace{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cluster.GetApplicationNamespace()}, updated)).Should(Succeed())
		g.Expect(updated.Labels[labels.SecurityEnforce]).To(Equal("baseline"))
		g.Expect(updated.Annotations).ShouldNot(HaveKey(annotations.PSAElevatedBy))
	})

	t.Run("no-op when label and annotation already match", func(t *testing.T) {
		ns := newNamespace(cluster.GetApplicationNamespace(), "privileged")
		ns.Annotations = map[string]string{annotations.PSAElevatedBy: "kserve-modelcache"}
		cli, err := fakeclient.New(fakeclient.WithObjects(ns))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = updateNamespacePSA(ctx, cli, "privileged")
		g.Expect(err).ShouldNot(HaveOccurred())

		updated := &corev1.Namespace{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cluster.GetApplicationNamespace()}, updated)).Should(Succeed())
		g.Expect(updated.Labels[labels.SecurityEnforce]).To(Equal("privileged"))
		g.Expect(updated.Annotations[annotations.PSAElevatedBy]).To(Equal("kserve-modelcache"))
	})
}

func TestReconcileModelCache(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	newNamespace := func(psaLevel string) *corev1.Namespace {
		return &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: cluster.GetApplicationNamespace(),
				Labels: map[string]string{
					labels.SecurityEnforce: psaLevel,
				},
			},
		}
	}

	runModelCachePSATest := func(t *testing.T, initialPSA string, expectedPSA string) {
		t.Helper()
		ns := newNamespace(initialPSA)
		cacheSize := resource.MustParse("100Gi")

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "worker-1",
				Labels: map[string]string{},
			},
		}

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kserveConfigMapName,
				Namespace: cluster.GetApplicationNamespace(),
			},
			Data: map[string]string{
				OpenshiftConfigKeyName: `{"modelcachePermissionFixImage": "old-image"}`,
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(ns, node, cm))
		g.Expect(err).ShouldNot(HaveOccurred())

		kserve := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.KserveInstanceName,
			},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Managed,
						CacheSize:       &cacheSize,
						NodeNames:       []string{"worker-1"},
					},
				},
			},
		}

		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: kserve,
		}

		err = reconcileModelCache(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		updated := &corev1.Namespace{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cluster.GetApplicationNamespace()}, updated)).Should(Succeed())
		g.Expect(updated.Labels[labels.SecurityEnforce]).To(Equal(expectedPSA))
	}

	t.Run("ModelCache Managed sets privileged PSA", func(t *testing.T) {
		runModelCachePSATest(t, "baseline", "privileged")
	})

	t.Run("ModelCache Managed baseline already correct is no-op for PSA", func(t *testing.T) {
		runModelCachePSATest(t, "privileged", "privileged")
	})

	runModelCacheCleanupTest := func(t *testing.T, modelCache *componentApi.ModelCacheSpec) {
		t.Helper()
		g := NewWithT(t)
		ctx := t.Context()

		ns := newNamespace("privileged")
		ns.Annotations = map[string]string{annotations.PSAElevatedBy: "kserve-modelcache"}

		pv := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "kserve-localmodelnode-pv"},
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kserve-localmodelnode-pvc",
				Namespace: cluster.GetApplicationNamespace(),
			},
		}

		lmng := &unstructured.Unstructured{}
		lmng.SetGroupVersionKind(gvk.LocalModelNodeGroup)
		lmng.SetName("workers")
		lmng.Object["spec"] = map[string]interface{}{"storageLimit": "100Gi"}

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "worker-1",
				Labels: map[string]string{modelCacheLabelKey: modelCacheLabelValue},
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(ns, pv, pvc, lmng, node))
		g.Expect(err).ShouldNot(HaveOccurred())

		kserveObj := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: modelCache,
				},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: kserveObj}

		err = reconcileModelCache(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "kserve-localmodelnode-pv"}, &corev1.PersistentVolume{})).
			Should(MatchError(ContainSubstring("not found")))

		g.Expect(cli.Get(ctx, client.ObjectKey{
			Name: "kserve-localmodelnode-pvc", Namespace: cluster.GetApplicationNamespace(),
		}, &corev1.PersistentVolumeClaim{})).
			Should(MatchError(ContainSubstring("not found")))

		lmngCheck := &unstructured.Unstructured{}
		lmngCheck.SetGroupVersionKind(gvk.LocalModelNodeGroup)
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "workers"}, lmngCheck)).
			Should(MatchError(ContainSubstring("not found")))

		updatedNode := &corev1.Node{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "worker-1"}, updatedNode)).Should(Succeed())
		g.Expect(updatedNode.Labels).ShouldNot(HaveKey(modelCacheLabelKey))

		updatedNs := &corev1.Namespace{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cluster.GetApplicationNamespace()}, updatedNs)).Should(Succeed())
		g.Expect(updatedNs.Labels[labels.SecurityEnforce]).To(Equal("baseline"))
		g.Expect(updatedNs.Annotations).ShouldNot(HaveKey(annotations.PSAElevatedBy))
	}

	t.Run("ModelCache Removed cleans up all resources", func(t *testing.T) {
		runModelCacheCleanupTest(t, &componentApi.ModelCacheSpec{
			ManagementState: operatorv1.Removed,
		})
	})

	t.Run("ModelCache nil cleans up all resources", func(t *testing.T) {
		runModelCacheCleanupTest(t, nil)
	})

	t.Run("ModelCache Removed is idempotent when resources already gone", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		ns := newNamespace("baseline")

		cli, err := fakeclient.New(fakeclient.WithObjects(ns))
		g.Expect(err).ShouldNot(HaveOccurred())

		kserveObj := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
			Spec: componentApi.KserveSpec{
				KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelCache: &componentApi.ModelCacheSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		}
		rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: kserveObj}

		err = reconcileModelCache(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		updatedNs := &corev1.Namespace{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cluster.GetApplicationNamespace()}, updatedNs)).Should(Succeed())
		g.Expect(updatedNs.Labels[labels.SecurityEnforce]).To(Equal("baseline"))
	})
}
