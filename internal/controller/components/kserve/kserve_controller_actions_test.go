//nolint:testpackage
package kserve

import (
	"encoding/json"
	"testing"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
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

func TestCheckSubscriptionDependencies(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	const happyCondition = "Ready"

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	t.Run("RHCL subscription absent sets LLMInferenceServiceDependencies to False", func(t *testing.T) {
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
	})

	t.Run("RHCL subscription present sets LLMInferenceServiceDependencies to True", func(t *testing.T) {
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
		g.Expect(got.Status).Should(Equal(metav1.ConditionTrue))
	})

	t.Run("Only LWS absent sets LLMInferenceServiceWideEPDependencies to False", func(t *testing.T) {
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

		// RHCL is present, so LLMInferenceServiceDependencies should be True
		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionTrue))

		// LWS is absent, so LLMInferenceServiceWideEPDependencies should be False
		gotWide := condManager.GetCondition(LLMInferenceServiceWideEPDependencies)
		g.Expect(gotWide).ShouldNot(BeNil())
		g.Expect(gotWide.Status).Should(Equal(metav1.ConditionFalse))
		g.Expect(gotWide.Message).Should(ContainSubstring("LeaderWorkerSet"))
		g.Expect(gotWide.Message).ShouldNot(ContainSubstring("Red Hat Connectivity Link"))
	})

	t.Run("Both RHCL and LWS present sets both conditions to True", func(t *testing.T) {
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

		got := condManager.GetCondition(LLMInferenceServiceDependencies)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionTrue))

		gotWide := condManager.GetCondition(LLMInferenceServiceWideEPDependencies)
		g.Expect(gotWide).ShouldNot(BeNil())
		g.Expect(gotWide.Status).Should(Equal(metav1.ConditionTrue))
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

func TestCheckOperatorAndCRDDependencies(t *testing.T) {
	const happyCondition = "Ready"

	// All CRD GVKs that the action monitors on Kubernetes clusters.
	monitoredCRDGVKs := []string{
		gvk.DestinationRule.Kind,
		gvk.EnvoyFilter.Kind,
		gvk.IstioGateway.Kind,
		gvk.ProxyConfig.Kind,
		gvk.ServiceEntry.Kind,
		gvk.Sidecar.Kind,
		gvk.WorkloadEntry.Kind,
		gvk.WorkloadGroup.Kind,
		gvk.AuthorizationPolicy.Kind,
		gvk.PeerAuthentication.Kind,
		gvk.RequestAuthentication.Kind,
		gvk.Telemetry.Kind,
		gvk.WasmPlugin.Kind,
		gvk.CertManagerCertificate.Kind,
		gvk.CertManagerCertificateRequest.Kind,
		gvk.CertManagerIssuer.Kind,
		gvk.CertManagerClusterIssuer.Kind,
		gvk.LeaderWorkerSetV1.Kind,
	}

	t.Run("Kubernetes cluster with missing CRDs sets DependenciesAvailable to False", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition, status.ConditionDependenciesAvailable)
		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkOperatorAndCRDDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		got := condManager.GetCondition(status.ConditionDependenciesAvailable)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionFalse))

		for _, kind := range monitoredCRDGVKs {
			g.Expect(got.Message).Should(ContainSubstring(kind), "expected message to mention %s", kind)
		}
	})

	t.Run("Kubernetes cluster with all CRDs present sets DependenciesAvailable to True", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		allMonitoredGVKs := []schema.GroupVersionKind{
			gvk.DestinationRule,
			gvk.EnvoyFilter,
			gvk.IstioGateway,
			gvk.ProxyConfig,
			gvk.ServiceEntry,
			gvk.Sidecar,
			gvk.WorkloadEntry,
			gvk.WorkloadGroup,
			gvk.AuthorizationPolicy,
			gvk.PeerAuthentication,
			gvk.RequestAuthentication,
			gvk.Telemetry,
			gvk.WasmPlugin,
			gvk.CertManagerCertificate,
			gvk.CertManagerCertificateRequest,
			gvk.CertManagerIssuer,
			gvk.CertManagerClusterIssuer,
			gvk.LeaderWorkerSetV1,
		}

		cli, err := fakeclientWithCRDs(allMonitoredGVKs)
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition, status.ConditionDependenciesAvailable)
		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkOperatorAndCRDDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		got := condManager.GetCondition(status.ConditionDependenciesAvailable)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionTrue))
	})

	t.Run("OpenShift cluster skips CRD checks and sets DependenciesAvailable to True", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		instance := &componentApi.Kserve{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
		}

		condManager := cond.NewManager(instance, happyCondition, status.ConditionDependenciesAvailable)
		rr := &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   instance,
			Conditions: condManager,
		}

		action := checkOperatorAndCRDDependencies()
		err = action(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		got := condManager.GetCondition(status.ConditionDependenciesAvailable)
		g.Expect(got).ShouldNot(BeNil())
		g.Expect(got.Status).Should(Equal(metav1.ConditionTrue))
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

// fakeclientWithCRDs builds a fake client whose RESTMapper knows about the
// given GVKs and that contains matching CRD objects with StoredVersions set,
// so that cluster.HasCRD returns true for each of them.
func fakeclientWithCRDs(gvks []schema.GroupVersionKind) (client.Client, error) {
	s, err := testscheme.New()
	if err != nil {
		return nil, err
	}

	fakeMapper := meta.NewDefaultRESTMapper(s.PreferredVersionAllGroups())
	for kt := range s.AllKnownTypes() {
		fakeMapper.Add(kt, meta.RESTScopeNamespace)
	}

	crdObjs := make([]client.Object, 0, len(gvks))
	for _, item := range gvks {
		fakeMapper.Add(item, meta.RESTScopeNamespace)

		plural, _ := meta.UnsafeGuessKindToResource(item)
		crdName := plural.Resource + "." + item.Group

		crdObjs = append(crdObjs, &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: crdName},
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				StoredVersions: []string{item.Version},
			},
		})
	}

	return clientFake.NewClientBuilder().
		WithScheme(s).
		WithRESTMapper(fakeMapper).
		WithObjects(crdObjs...).
		Build(), nil
}
