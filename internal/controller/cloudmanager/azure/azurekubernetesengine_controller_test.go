package azure_test

import (
	"context"
	"io"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/azure"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	ccmtest "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const nsCertManagerOperator = "cert-manager-operator"

var azureCfg = ccmtest.ControllerTestConfig{
	CRDSubdir:     "azure",
	NewReconciler: azure.NewReconciler,
	NewCR: func(deps ccmcommon.Dependencies) client.Object {
		return &ccmv1alpha1.AzureKubernetesEngine{
			ObjectMeta: metav1.ObjectMeta{
				Name: ccmv1alpha1.AzureKubernetesEngineInstanceName,
			},
			Spec: ccmv1alpha1.AzureKubernetesEngineSpec{
				Dependencies: deps,
			},
		}
	},
	InstanceName: ccmv1alpha1.AzureKubernetesEngineInstanceName,
	InfraLabel:   "azurekubernetesengine",
	GVK:          gvk.AzureKubernetesEngine,
}

func TestAzureKubernetesEngine(t *testing.T) { //nolint:maintidx
	ccmtest.RequireCharts(t)

	t.Run("stamps InfrastructureDependency label on chart-specific resources", func(t *testing.T) {
		wt := tc.NewWithT(t)

		ccmtest.CreateCR(t, wt, azureCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		// Each chart's resources must carry the InfrastructureDependency label whose value
		// is the chart's ReleaseName. The GC predicate and cleanupOwnership derive
		// Managed/Unmanaged state from this label.
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(
			jq.Match(`.metadata.labels."%s" == "cert-manager-operator"`, labels.InfrastructureDependency),
		)
	})

	t.Run("removes owner refs from resources of dependency that transitions to Unmanaged", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		_, wt := ccmtest.StartIsolatedController(t, ctx, azureCfg)
		t.Cleanup(cancel)

		// Start with cert-manager Managed — the controller deploys it and sets owner refs.
		ccmtest.CreateCR(t, wt, azureCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		// Wait until the cert-manager Deployment exists and has an owner ref from the AKE CR.
		// The deploy action sets ctrl.SetControllerReference when dynamic ownership is enabled.
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(
			jq.Match(`.metadata.ownerReferences | length > 0`),
		)

		// Transition cert-manager to Unmanaged. The next reconcile runs cleanupOwnership,
		// which removes owner refs from cert-manager resources so they survive CR deletion.
		ake := &ccmv1alpha1.AzureKubernetesEngine{}
		wt.Expect(wt.Client().Get(wt.Context(),
			types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}, ake)).To(Succeed())
		ake.Spec.Dependencies.CertManager.ManagementPolicy = ccmcommon.Unmanaged
		wt.Expect(wt.Client().Update(wt.Context(), ake)).To(Succeed())

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(
			jq.Match(`.metadata.ownerReferences | length == 0`),
		)
	})

	t.Run("GC deletes stale resources with mismatched generation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wt := ccmtest.StartIsolatedController(t, ctx, azureCfg)
		t.Cleanup(cancel)

		// Create the AKE CR — after the first reconcile, the CR gets a real UID and generation.
		ccmtest.CreateCR(t, wt, azureCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		// Wait for the CR to be reconciled (cert-manager deployment appears, which means
		// the reconcile ran and the CR has a non-zero UID and generation).
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(Not(BeNil()))

		// Fetch the AKE CR to obtain its UID.
		ake := &ccmv1alpha1.AzureKubernetesEngine{}
		wt.Expect(et.Client().Get(ctx,
			types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}, ake)).To(Succeed())

		// Create a ConfigMap that looks like a stale CCM resource (wrong generation).
		// The GC predicate will find it via the InfrastructurePartOf label, check
		// UID (matches), check generation (mismatch) → delete it.
		ns := nsCertManagerOperator
		staleCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stale-ccm-resource",
				Namespace: ns,
				Labels: map[string]string{
					labels.InfrastructurePartOf:     "azurekubernetesengine",
					labels.InfrastructureDependency: "cert-manager-operator",
				},
				Annotations: map[string]string{
					annotations.InstanceUID: string(ake.GetUID()),
					// A generation far in the past — will never match the current CR generation.
					annotations.InstanceGeneration: strconv.FormatInt(-1, 10),
				},
			},
		}
		wt.Expect(et.Client().Create(ctx, staleCM)).To(Succeed())
		t.Cleanup(func() {
			_ = et.Client().Delete(ctx, staleCM)
		})

		// Trigger a spec change to cause a cache miss → GC runs.
		ake.Spec.Dependencies.LWS.ManagementPolicy = ccmcommon.Managed
		wt.Expect(et.Client().Update(ctx, ake)).To(Succeed())

		// GC should delete the stale resource. In envtest there is no garbage collector
		// process, so Foreground deletion marks the object with a deletionTimestamp but
		// does not remove it. Either outcome (gone or marked for deletion) confirms the
		// GC predicate fired correctly.
		wt.Get(gvk.ConfigMap, client.ObjectKeyFromObject(staleCM)).Eventually().Should(
			Or(BeNil(), jq.Match(`.metadata.deletionTimestamp != null`)),
		)
	})

	t.Run("GC keeps retain-labeled resources regardless of generation mismatch", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wt := ccmtest.StartIsolatedController(t, ctx, azureCfg)
		t.Cleanup(cancel)

		ccmtest.CreateCR(t, wt, azureCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(Not(BeNil()))

		ake := &ccmv1alpha1.AzureKubernetesEngine{}
		wt.Expect(et.Client().Get(ctx,
			types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}, ake)).To(Succeed())

		// Create a ConfigMap labelled as a lifecycle-independent CCM resource with a stale
		// generation. GC must keep it because the InfrastructureGCPolicy=retain label takes
		// precedence over the generation check.
		ns := nsCertManagerOperator
		retainCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "retain-labeled-resource",
				Namespace: ns,
				Labels: map[string]string{
					labels.InfrastructurePartOf:   "azurekubernetesengine",
					labels.InfrastructureGCPolicy: labels.GCPolicyRetain,
				},
				Annotations: map[string]string{
					annotations.InstanceUID:        string(ake.GetUID()),
					annotations.InstanceGeneration: strconv.FormatInt(-1, 10),
				},
			},
		}
		wt.Expect(et.Client().Create(ctx, retainCM)).To(Succeed())
		t.Cleanup(func() { _ = et.Client().Delete(ctx, retainCM) })

		// Trigger a spec change → cache miss → GC runs.
		ake.Spec.Dependencies.LWS.ManagementPolicy = ccmcommon.Managed
		wt.Expect(et.Client().Update(ctx, ake)).To(Succeed())

		// Wait for the LWS deployment to confirm the reconcile (and GC) completed.
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "openshift-lws-operator", Namespace: "openshift-lws-operator",
		}).Eventually().Should(Not(BeNil()))

		// The retain-labeled resource must survive across GC runs. Consistently checks
		// that the resource is not deleted over time, not just at one point in time.
		NewWithT(t).Consistently(func() error {
			return et.Client().Get(ctx, client.ObjectKeyFromObject(retainCM), &corev1.ConfigMap{})
		}).WithTimeout(5 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())
	})

	t.Run("GC keeps retain-labeled resources even with UID mismatch", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wt := ccmtest.StartIsolatedController(t, ctx, azureCfg)
		t.Cleanup(cancel)

		ccmtest.CreateCR(t, wt, azureCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(Not(BeNil()))

		// Create a retain-labeled ConfigMap whose InstanceUID belongs to a different CR instance.
		// The retain check runs before the UID check, so the resource is always kept.
		ns := nsCertManagerOperator
		orphanCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "orphan-retain-resource",
				Namespace: ns,
				Labels: map[string]string{
					labels.InfrastructurePartOf:   "azurekubernetesengine",
					labels.InfrastructureGCPolicy: labels.GCPolicyRetain,
				},
				Annotations: map[string]string{
					annotations.InstanceUID:        "00000000-0000-0000-0000-000000000000",
					annotations.InstanceGeneration: "1",
				},
			},
		}
		wt.Expect(et.Client().Create(ctx, orphanCM)).To(Succeed())
		t.Cleanup(func() { _ = et.Client().Delete(ctx, orphanCM) })

		ake := &ccmv1alpha1.AzureKubernetesEngine{}
		wt.Expect(et.Client().Get(ctx,
			types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}, ake)).To(Succeed())

		// Trigger a spec change → cache miss → GC runs.
		ake.Spec.Dependencies.LWS.ManagementPolicy = ccmcommon.Managed
		wt.Expect(et.Client().Update(ctx, ake)).To(Succeed())

		// Wait for LWS deployment to confirm GC ran, then verify the retain-labeled resource survived.
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "openshift-lws-operator", Namespace: "openshift-lws-operator",
		}).Eventually().Should(Not(BeNil()))

		NewWithT(t).Consistently(func() error {
			return et.Client().Get(ctx, client.ObjectKeyFromObject(orphanCM), &corev1.ConfigMap{})
		}).WithTimeout(5 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())
	})

	t.Run("deploys managed dependencies", func(t *testing.T) {
		wt := tc.NewWithT(t)

		ccmtest.CreateCR(t, wt, azureCfg, ccmcommon.Dependencies{
			GatewayAPI:   ccmcommon.GatewayAPIDependency{ManagementPolicy: ccmcommon.Managed},
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		})

		// Verify dependency deployments are created
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(Not(BeNil()))

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "openshift-lws-operator", Namespace: "openshift-lws-operator",
		}).Eventually().Should(Not(BeNil()))

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "servicemesh-operator3", Namespace: "istio-system",
		}).Eventually().Should(Not(BeNil()))
	})

	t.Run("sets infrastructure label on deployed resources", func(t *testing.T) {
		wt := tc.NewWithT(t)

		ccmtest.CreateCR(t, wt, azureCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(
			jq.Match(`.metadata.labels."%s" == "azurekubernetesengine"`, labels.InfrastructurePartOf),
		)
	})

	t.Run("creates PKI bootstrap resources when cert-manager is installed", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wtC := ccmtest.StartIsolatedController(t, ctx, azureCfg)
		t.Cleanup(cancel) // stop the manager before the test environment (registered after et.Stop, so it runs first)

		_, err := et.RegisterCertManagerCRDs(ctx, envt.WithPermissiveSchema())
		wtC.Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}}
		if err := et.Client().Create(ctx, ns); err != nil && !k8serr.IsAlreadyExists(err) {
			wtC.Expect(err).NotTo(HaveOccurred())
		}

		ccmtest.CreateCR(t, wtC, azureCfg, ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		})

		nn := types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}

		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-selfsigned-issuer"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerCertificate, types.NamespacedName{Name: "opendatahub-ca", Namespace: "cert-manager"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-ca-issuer"}).
			Eventually().ShouldNot(BeNil())

		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "True"`),
		)
	})
}

// TestAzureKubernetesEngineWithoutCertManager tests cert-manager CRD absence and dynamic
// registration. Each sub-test uses an isolated envtest to start with zero cert-manager CRDs.
func TestAzureKubernetesEngineWithoutCertManager(t *testing.T) {
	ccmtest.RequireCharts(t)

	logf.SetLogger(zap.New(zap.WriteTo(io.Discard), zap.UseDevMode(true)))

	t.Run("reports DependenciesAvailable=False and Ready=False when cert-manager CRDs absent", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		_, wtC := ccmtest.StartIsolatedController(t, ctx, azureCfg)
		t.Cleanup(cancel) // stop the manager before the test environment (registered after et.Stop, so it runs first)

		nn := types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}
		ccmtest.CreateCR(t, wtC, azureCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "False"`),
		)
		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "False"`),
		)
	})

	t.Run("reconciles PKI resources after cert-manager CRDs appear", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wtC := ccmtest.StartIsolatedController(t, ctx, azureCfg)
		t.Cleanup(cancel) // stop the manager before the test environment (registered after et.Stop, so it runs first)
		nn := types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}

		ccmtest.CreateCR(t, wtC, azureCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "False"`),
		)

		_, err := et.RegisterCertManagerCRDs(ctx, envt.WithPermissiveSchema())
		wtC.Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}}
		if err := et.Client().Create(ctx, ns); err != nil && !k8serr.IsAlreadyExists(err) {
			wtC.Expect(err).NotTo(HaveOccurred())
		}

		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "True"`),
		)
		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-selfsigned-issuer"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerCertificate, types.NamespacedName{Name: "opendatahub-ca", Namespace: "cert-manager"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-ca-issuer"}).
			Eventually().ShouldNot(BeNil())
	})
}
