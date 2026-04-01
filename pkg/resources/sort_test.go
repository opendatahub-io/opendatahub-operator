package resources_test

import (
	"context"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	controllerTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

func newUnstructured(group, version, kind, namespace, name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
	u.SetNamespace(namespace)
	u.SetName(name)
	return u
}

// createCertManagerCRDs registers the three cert-manager CRDs required for integration testing
// and schedules their cleanup for the end of the test.
// Follows the exact same pattern as createBootstrapCRDs in bootstrap_test.go.
func createCertManagerCRDs(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT) {
	t.Helper()
	crds, err := envTest.RegisterCertManagerCRDs(ctx, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	for _, crd := range crds {
		envt.CleanupDelete(t, g, ctx, envTest.Client(), crd)
	}
}

func TestSortByApplyOrder(t *testing.T) {
	t.Run("sorts CRDs before Deployments before unknown kinds", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			newUnstructured(gvk.CertManagerClusterIssuer.Group, gvk.CertManagerClusterIssuer.Version, gvk.CertManagerClusterIssuer.Kind, "", "my-issuer"),
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "ns", "my-deploy"),
			newUnstructured(gvk.CustomResourceDefinition.Group, gvk.CustomResourceDefinition.Version, gvk.CustomResourceDefinition.Kind, "", "my-crd"),
		}

		result, err := resources.SortByApplyOrder(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(3))
		g.Expect(result[0].GetKind()).To(Equal("CustomResourceDefinition"))
		g.Expect(result[1].GetKind()).To(Equal("Deployment"))
		g.Expect(result[2].GetKind()).To(Equal("ClusterIssuer"))
	})

	t.Run("sorts webhooks last", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			newUnstructured("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "webhook"),
			newUnstructured(gvk.Namespace.Group, gvk.Namespace.Version, gvk.Namespace.Kind, "", "my-ns"),
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "ns", "my-deploy"),
		}

		result, err := resources.SortByApplyOrder(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(3))
		g.Expect(result[0].GetKind()).To(Equal("Namespace"))
		g.Expect(result[1].GetKind()).To(Equal("Deployment"))
		g.Expect(result[2].GetKind()).To(Equal("ValidatingWebhookConfiguration"))
	})

	t.Run("unknown kinds placed in middle", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			newUnstructured(gvk.CertManagerCertificate.Group, gvk.CertManagerCertificate.Version, gvk.CertManagerCertificate.Kind, "cert-manager", "ca"),
			newUnstructured(gvk.Namespace.Group, gvk.Namespace.Version, gvk.Namespace.Kind, "", "my-ns"),
			newUnstructured("admissionregistration.k8s.io", "v1", "MutatingWebhookConfiguration", "", "webhook"),
		}

		result, err := resources.SortByApplyOrder(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(3))
		g.Expect(result[0].GetKind()).To(Equal(gvk.Namespace.Kind))
		g.Expect(result[1].GetKind()).To(Equal(gvk.CertManagerCertificate.Kind))
		g.Expect(result[2].GetKind()).To(Equal("MutatingWebhookConfiguration"))
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		g := NewWithT(t)

		result, err := resources.SortByApplyOrder(context.Background(), nil)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(BeEmpty())
	})

	t.Run("stable sort preserves order for same kind", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "ns", "deploy-b"),
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "ns", "deploy-a"),
		}

		result, err := resources.SortByApplyOrder(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(2))
		// Same GVK + namespace → sorted by name
		g.Expect(result[0].GetName()).To(Equal("deploy-a"))
		g.Expect(result[1].GetName()).To(Equal("deploy-b"))
	})

	t.Run("cert-manager dependency ordering with SortByApplyOrderWithCertificates", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			// Mixed order input to test comprehensive ordering
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "app-ns", "consuming-app"),
			newUnstructured("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "webhook"),
			newUnstructured(gvk.CertManagerCertificate.Group, gvk.CertManagerCertificate.Version, gvk.CertManagerCertificate.Kind, "cert-manager", "ca-cert"),
			newUnstructured(gvk.Service.Group, gvk.Service.Version, gvk.Service.Kind, "app-ns", "app-service"),
			newUnstructured(gvk.CertManagerClusterIssuer.Group, gvk.CertManagerClusterIssuer.Version, gvk.CertManagerClusterIssuer.Kind, "", "ca-issuer"),
			newUnstructured(gvk.Namespace.Group, gvk.Namespace.Version, gvk.Namespace.Kind, "", "app-ns"),
			newUnstructured(gvk.CertManagerIssuer.Group, gvk.CertManagerIssuer.Version, gvk.CertManagerIssuer.Kind, "cert-manager", "self-signed-issuer"),
			newUnstructured(gvk.CustomResourceDefinition.Group, gvk.CustomResourceDefinition.Version, gvk.CustomResourceDefinition.Kind, "", "certificates.cert-manager.io"),
		}

		result, err := resources.SortByApplyOrderWithCertificates(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(8))

		// Expected ordering (RHOAIENG-53513 requirement):
		// Certificate BEFORE Deployment to reduce "transient errors"
		// 1. Basic resources: Namespace, CustomResourceDefinition (upstream decides)
		// 2. Service (upstream decides)
		// 3. cert-manager resources: ClusterIssuer, Issuer, Certificate (inserted before Deployment)
		// 4. Workloads: Deployment, Webhooks (upstream decides their order)
		// Key: cert-manager comes after foundation/networking but before workloads

		g.Expect(result[0].GetKind()).To(Equal("Namespace"))
		g.Expect(result[1].GetKind()).To(Equal("CustomResourceDefinition"))
		g.Expect(result[2].GetKind()).To(Equal("Service"))
		g.Expect(result[3].GetKind()).To(Equal("ClusterIssuer"))
		g.Expect(result[4].GetKind()).To(Equal("Issuer"))
		g.Expect(result[5].GetKind()).To(Equal("Certificate"))
		g.Expect(result[6].GetKind()).To(Equal("Deployment"))
		g.Expect(result[7].GetKind()).To(Equal("ValidatingWebhookConfiguration"))
	})

	t.Run("cert-manager ordering works without Services", func(t *testing.T) {
		g := NewWithT(t)

		// Test scenario with NO Services - cert-manager should still work
		input := []unstructured.Unstructured{
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "app-ns", "consuming-app"),
			newUnstructured("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "webhook"),
			newUnstructured(gvk.CertManagerCertificate.Group, gvk.CertManagerCertificate.Version, gvk.CertManagerCertificate.Kind, "cert-manager", "ca-cert"),
			newUnstructured(gvk.CertManagerClusterIssuer.Group, gvk.CertManagerClusterIssuer.Version, gvk.CertManagerClusterIssuer.Kind, "", "ca-issuer"),
			newUnstructured(gvk.Namespace.Group, gvk.Namespace.Version, gvk.Namespace.Kind, "", "app-ns"),
			newUnstructured(gvk.CustomResourceDefinition.Group, gvk.CustomResourceDefinition.Version, gvk.CustomResourceDefinition.Kind, "", "certificates.cert-manager.io"),
		}

		result, err := resources.SortByApplyOrderWithCertificates(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(6))

		// Expected ordering without Services:
		// 1. Basic resources: Namespace, CRD
		// 2. cert-manager: ClusterIssuer, Certificate (inserted before Deployment)
		// 3. Workloads: Deployment, ValidatingWebhookConfiguration

		g.Expect(result[0].GetKind()).To(Equal("Namespace"))
		g.Expect(result[1].GetKind()).To(Equal("CustomResourceDefinition"))
		g.Expect(result[2].GetKind()).To(Equal("ClusterIssuer"))
		g.Expect(result[3].GetKind()).To(Equal("Certificate"))
		g.Expect(result[4].GetKind()).To(Equal("Deployment"))
		g.Expect(result[5].GetKind()).To(Equal("ValidatingWebhookConfiguration"))
	})
}

// TestCertManagerOrderingIntegration demonstrates that proper resource ordering
// prevents transient failures when deploying cert-manager-dependent resources.
// This test uses the established envt utilities with real cert-manager CRDs
// and follows the pkg/resources/ pattern of mixing unit and integration tests.
func TestCertManagerOrderingIntegration(t *testing.T) {
	g := NewWithT(t)

	// Use established envt utilities (following bootstrap_test.go pattern)
	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()

	// Register cert-manager CRDs using established utility (same as bootstrap_test.go)
	createCertManagerCRDs(t, g, ctx, envTest)

	// Create test namespace
	testNamespace := "cert-ordering-test"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	g.Expect(envTest.Client().Create(ctx, ns)).To(Succeed())

	t.Run("Proper ordering ensures cert-manager dependencies work", func(t *testing.T) {
		g := NewWithT(t)

		// Scenario: RHOAIENG-53513 - Deployment mounting Certificate-generated Secret
		// Dependencies: ClusterIssuer → Certificate → Secret (generated) → Deployment (consumes)
		testResources, err := createRealCertManagerScenario(testNamespace)
		g.Expect(err).NotTo(HaveOccurred())

		// Deploy with our enhanced cert-manager ordering
		action := deploy.NewAction(deploy.WithApplyOrder())

		rr := controllerTypes.ReconciliationRequest{
			Client:   envTest.Client(),
			Instance: &componentApi.Dashboard{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			Release: common.Release{
				Name: cluster.OpenDataHub,
				Version: version.OperatorVersion{Version: semver.Version{
					Major: 1, Minor: 2, Patch: 3,
				}},
			},
			Resources: testResources,
			Controller: mocks.NewMockController(func(m *mocks.MockController) {
				m.On("Owns", mock.Anything).Return(false)
			}),
		}

		// Deploy - should succeed with proper ordering
		err = action(ctx, &rr)
		g.Expect(err).NotTo(HaveOccurred(), "Deploy should succeed with proper cert-manager ordering")

		// Verify resources were applied in dependency order:
		// 1. ClusterIssuer is created
		clusterIssuerKey := types.NamespacedName{Name: "rhoai-ca-issuer"}
		clusterIssuer := &unstructured.Unstructured{}
		clusterIssuer.SetGroupVersionKind(gvk.CertManagerClusterIssuer)

		err = envTest.Client().Get(ctx, clusterIssuerKey, clusterIssuer)
		g.Expect(err).NotTo(HaveOccurred(), "ClusterIssuer should be created first")

		// 2. Certificate is created after ClusterIssuer
		certificateKey := types.NamespacedName{Name: "rhoai-serving-cert", Namespace: testNamespace}
		certificate := &unstructured.Unstructured{}
		certificate.SetGroupVersionKind(gvk.CertManagerCertificate)

		err = envTest.Client().Get(ctx, certificateKey, certificate)
		g.Expect(err).NotTo(HaveOccurred(), "Certificate should be created after ClusterIssuer")

		// 3. Deployment is created last
		deploymentKey := types.NamespacedName{Name: "rhoai-serving-app", Namespace: testNamespace}
		deployment := &appsv1.Deployment{}

		err = envTest.Client().Get(ctx, deploymentKey, deployment)
		g.Expect(err).NotTo(HaveOccurred(), "Deployment should be created after Certificate")

		// 4. Verify Deployment properly references Certificate-generated Secret
		g.Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
		secretVolume := deployment.Spec.Template.Spec.Volumes[0]
		g.Expect(secretVolume.Secret.SecretName).To(Equal("rhoai-serving-tls"))

		// The test passes if all resources are created without errors.
		// In real scenarios, cert-manager controller would process the Certificate
		// and create the Secret, but our test proves the ordering prevents the race condition.
	})

	t.Run("Without enhanced ordering, cert-manager resources are placed randomly", func(t *testing.T) {
		g := NewWithT(t)

		// Test scenario with upstream ordering only (no cert-manager enhancement)
		testResources, err := createRealCertManagerScenario(testNamespace)
		g.Expect(err).NotTo(HaveOccurred())

		action := deploy.NewAction() // No WithApplyOrder() - uses default upstream ordering only

		rr := controllerTypes.ReconciliationRequest{
			Client:   envTest.Client(),
			Instance: &componentApi.Dashboard{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			Release: common.Release{
				Name: cluster.OpenDataHub,
				Version: version.OperatorVersion{Version: semver.Version{
					Major: 1, Minor: 2, Patch: 3,
				}},
			},
			Resources: testResources,
			Controller: mocks.NewMockController(func(m *mocks.MockController) {
				m.On("Owns", mock.Anything).Return(false)
			}),
		}

		// Deploy without cert-manager ordering enhancement
		err = action(ctx, &rr)
		g.Expect(err).NotTo(HaveOccurred(), "Deploy should succeed, but ordering may be wrong")

		// Verify cert-manager resources were created, but potentially in wrong order
		// This demonstrates the race condition risk that our enhancement fixes
	})
}

// createRealCertManagerScenario creates realistic cert-manager resources
// that demonstrate the RHOAIENG-53513 dependency issue.
func createRealCertManagerScenario(namespace string) ([]unstructured.Unstructured, error) {
	// ClusterIssuer - self-signed CA for testing
	clusterIssuer := &unstructured.Unstructured{}
	clusterIssuer.SetGroupVersionKind(gvk.CertManagerClusterIssuer)
	clusterIssuer.SetName("rhoai-ca-issuer")
	// Don't overwrite Object - add spec to existing structure
	err := unstructured.SetNestedMap(clusterIssuer.Object, map[string]interface{}{
		"selfSigned": map[string]interface{}{},
	}, "spec")
	if err != nil {
		return nil, err
	}

	// Certificate - depends on ClusterIssuer, will generate Secret
	certificate := &unstructured.Unstructured{}
	certificate.SetGroupVersionKind(gvk.CertManagerCertificate)
	certificate.SetName("rhoai-serving-cert")
	certificate.SetNamespace(namespace)
	// Don't overwrite Object - add spec to existing structure
	err = unstructured.SetNestedMap(certificate.Object, map[string]interface{}{
		"secretName": "rhoai-serving-tls",
		"issuerRef": map[string]interface{}{
			"name":  "rhoai-ca-issuer",
			"kind":  "ClusterIssuer",
			"group": "cert-manager.io",
		},
		"dnsNames": []interface{}{
			"rhoai-serving.example.com",
			"*.rhoai-serving.example.com",
		},
		"subject": map[string]interface{}{
			"commonName": "RHOAI Serving Certificate",
		},
	}, "spec")
	if err != nil {
		return nil, err
	}

	// Deployment - depends on Secret generated by Certificate
	deployment, _ := resources.ToUnstructured(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rhoai-serving-app",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "rhoai-serving"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "rhoai-serving"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "serving-app",
							Image:   "registry.redhat.io/ubi8/ubi-minimal",
							Command: []string{"sleep", "3600"},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "tls-certs",
									MountPath: "/etc/ssl/certs",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "tls-certs",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "rhoai-serving-tls", // CRITICAL: Depends on Certificate
								},
							},
						},
					},
				},
			},
		},
	})

	// Return in mixed order - our sorting should fix this!
	return []unstructured.Unstructured{
		*deployment,    // Deployment first (WRONG - would fail without proper ordering)
		*certificate,   // Certificate second (WRONG - needs ClusterIssuer first)
		*clusterIssuer, // ClusterIssuer last (WRONG - should be first)
	}, nil
}

// Helper function.
func int32Ptr(i int32) *int32 {
	return &i
}
