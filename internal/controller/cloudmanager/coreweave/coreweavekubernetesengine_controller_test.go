package coreweave_test

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

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/coreweave/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/coreweave"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	ccmtest "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const nsCertManagerOperator = "cert-manager-operator"

func hasCertManagerDeployments(wt *testf.WithT) bool {
	return ccmtest.HasInfraDeployments(wt, nsCertManagerOperator, ccmv1alpha1.CoreWeaveKubernetesEngineKind)
}

var coreweaveCfg = ccmtest.ControllerTestConfig{
	CRDSubdir:     "coreweave",
	NewReconciler: coreweave.NewReconciler,
	NewCR: func(deps ccmcommon.Dependencies) client.Object {
		return &ccmv1alpha1.CoreWeaveKubernetesEngine{
			ObjectMeta: metav1.ObjectMeta{
				Name: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName,
			},
			Spec: ccmv1alpha1.CoreWeaveKubernetesEngineSpec{
				Dependencies: deps,
			},
		}
	},
	InstanceName: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName,
	InfraLabel:   "coreweavekubernetesengine",
	GVK:          gvk.CoreWeaveKubernetesEngine,
}

func TestCoreWeaveKubernetesEngine(t *testing.T) {
	ccmtest.RequireCharts(t)

	t.Run("deploys managed dependencies", func(t *testing.T) {
		wt := tc.NewWithT(t)

		ccmtest.CreateCR(t, wt, coreweaveCfg, ccmcommon.Dependencies{
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

		ccmtest.CreateCR(t, wt, coreweaveCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(
			jq.Match(`.metadata.labels."%s" == "coreweavekubernetesengine"`, labels.InfrastructurePartOf),
		)
	})

	t.Run("creates PKI bootstrap resources when cert-manager is installed", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wtC := ccmtest.StartIsolatedController(t, ctx, coreweaveCfg)
		t.Cleanup(cancel) // stop the manager before the test environment (registered after et.Stop, so it runs first)

		_, err := et.RegisterCertManagerCRDs(ctx, envt.WithPermissiveSchema())
		wtC.Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}}
		if err := et.Client().Create(ctx, ns); err != nil && !k8serr.IsAlreadyExists(err) {
			wtC.Expect(err).NotTo(HaveOccurred())
		}

		ccmtest.CreateCR(t, wtC, coreweaveCfg, ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		})

		nn := types.NamespacedName{Name: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName}

		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-selfsigned-issuer"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerCertificate, types.NamespacedName{Name: "opendatahub-ca", Namespace: "cert-manager"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-ca-issuer"}).
			Eventually().ShouldNot(BeNil())

		wtC.Get(gvk.CoreWeaveKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "True"`),
		)
	})
}

// TestCoreWeaveKubernetesEngineWithoutCertManager tests cert-manager CRD absence and dynamic
// registration. Each sub-test uses an isolated envtest to start with zero cert-manager CRDs.
func TestCoreWeaveKubernetesEngineWithoutCertManager(t *testing.T) {
	ccmtest.RequireCharts(t)

	logf.SetLogger(zap.New(zap.WriteTo(io.Discard), zap.UseDevMode(true)))

	t.Run("reports DependenciesAvailable=False and Ready=False when cert-manager CRDs absent", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		_, wtC := ccmtest.StartIsolatedController(t, ctx, coreweaveCfg)
		t.Cleanup(cancel) // stop the manager before the test environment (registered after et.Stop, so it runs first)

		nn := types.NamespacedName{Name: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName}
		ccmtest.CreateCR(t, wtC, coreweaveCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wtC.Get(gvk.CoreWeaveKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "False"`),
		)
		wtC.Get(gvk.CoreWeaveKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "False"`),
		)
	})

	t.Run("reconciles PKI resources after cert-manager CRDs appear", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wtC := ccmtest.StartIsolatedController(t, ctx, coreweaveCfg)
		t.Cleanup(cancel) // stop the manager before the test environment (registered after et.Stop, so it runs first)

		nn := types.NamespacedName{Name: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName}

		ccmtest.CreateCR(t, wtC, coreweaveCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wtC.Get(gvk.CoreWeaveKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "False"`),
		)

		_, err := et.RegisterCertManagerCRDs(ctx, envt.WithPermissiveSchema())
		wtC.Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}}
		if err := et.Client().Create(ctx, ns); err != nil && !k8serr.IsAlreadyExists(err) {
			wtC.Expect(err).NotTo(HaveOccurred())
		}

		wtC.Get(gvk.CoreWeaveKubernetesEngine, nn).Eventually().Should(
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

// TestCoreWeaveKubernetesEngineGC tests garbage collection behavior. All subtests share a
// single isolated envtest to reduce startup overhead. They run sequentially and each
// creates/deletes its own CR via CreateCR cleanup. The "protected resources" subtest
// must be last because it permanently registers cert-manager CRDs.
func TestCoreWeaveKubernetesEngineGC(t *testing.T) {
	ccmtest.RequireCharts(t)

	ctx, cancel := context.WithCancel(context.Background())
	et, wt := ccmtest.StartIsolatedController(t, ctx, coreweaveCfg)
	t.Cleanup(cancel)

	t.Run("deletes resources of dependency that transitions to Unmanaged", func(t *testing.T) {
		// Start with cert-manager Managed — the controller deploys it.
		ccmtest.CreateCR(t, wt, coreweaveCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		// Wait until the cert-manager Deployment exists.
		wt.Eventually(func() bool { return hasCertManagerDeployments(wt) }).Should(BeTrue())

		// Transition cert-manager to Unmanaged. Helm no longer renders cert-manager
		// resources, so they retain stale generation annotations. GC deletes them.
		cke := &ccmv1alpha1.CoreWeaveKubernetesEngine{}
		wt.Expect(wt.Client().Get(wt.Context(),
			types.NamespacedName{Name: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName}, cke)).To(Succeed())
		cke.Spec.Dependencies.CertManager.ManagementPolicy = ccmcommon.Unmanaged
		wt.Expect(wt.Client().Update(wt.Context(), cke)).To(Succeed())

		wt.Eventually(func() bool {
			list, err := ccmtest.ListInfraDeployments(wt, nsCertManagerOperator, ccmv1alpha1.CoreWeaveKubernetesEngineKind)
			wt.Expect(err).NotTo(HaveOccurred())
			if len(list) == 0 {
				return true
			}
			for i := range list {
				if list[i].GetDeletionTimestamp() == nil {
					return false
				}
			}
			return true
		}).Should(BeTrue())
	})

	t.Run("GC deletes stale resources with mismatched generation", func(t *testing.T) {
		// Create the CKE CR — after the first reconcile, the CR gets a real UID and generation.
		ccmtest.CreateCR(t, wt, coreweaveCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		// Wait for the CR to be reconciled (cert-manager deployment appears, which means
		// the reconcile ran and the CR has a non-zero UID and generation).
		wt.Eventually(func() bool { return hasCertManagerDeployments(wt) }).Should(BeTrue())

		// Fetch the CKE CR to obtain its UID.
		cke := &ccmv1alpha1.CoreWeaveKubernetesEngine{}
		wt.Expect(wt.Client().Get(wt.Context(),
			types.NamespacedName{Name: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName}, cke)).To(Succeed())

		// Create a ConfigMap that looks like a stale owned CCM resource (wrong generation).
		// GC only processes owned resources, so the ConfigMap must have an owner reference
		// matching the CKE CR's GVK.
		staleCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stale-ccm-resource",
				Namespace: nsCertManagerOperator,
				Labels: map[string]string{
					labels.InfrastructurePartOf: "coreweavekubernetesengine",
				},
				Annotations: map[string]string{
					annotations.InstanceUID: string(cke.GetUID()),
					// A generation far in the past — will never match the current CR generation.
					annotations.InstanceGeneration: strconv.FormatInt(-1, 10),
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: gvk.CoreWeaveKubernetesEngine.GroupVersion().String(),
					Kind:       gvk.CoreWeaveKubernetesEngine.Kind,
					Name:       cke.GetName(),
					UID:        cke.GetUID(),
				}},
			},
		}
		wt.Expect(wt.Client().Create(wt.Context(), staleCM)).To(Succeed())
		t.Cleanup(func() {
			_ = wt.Client().Delete(wt.Context(), staleCM)
		})

		// Trigger a spec change to cause a cache miss → GC runs.
		cke.Spec.Dependencies.LWS.ManagementPolicy = ccmcommon.Managed
		wt.Expect(wt.Client().Update(wt.Context(), cke)).To(Succeed())

		// GC should delete the stale resource. In envtest there is no garbage collector
		// process, so Foreground deletion marks the object with a deletionTimestamp but
		// does not remove it. Either outcome (gone or marked for deletion) confirms the
		// GC predicate fired correctly.
		wt.Get(gvk.ConfigMap, client.ObjectKeyFromObject(staleCM)).Eventually().Should(
			Or(BeNil(), jq.Match(`.metadata.deletionTimestamp != null`)),
		)
	})

	t.Run("GC keeps protected resources regardless of generation mismatch", func(t *testing.T) {
		ccmtest.CreateCR(t, wt, coreweaveCfg, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wt.Eventually(func() bool { return hasCertManagerDeployments(wt) }).Should(BeTrue())

		// Register cert-manager CRDs so we can create actual ClusterIssuer resources.
		_, err := et.RegisterCertManagerCRDs(wt.Context(), envt.WithPermissiveSchema())
		wt.Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}}
		if err := wt.Client().Create(wt.Context(), ns); err != nil && !k8serr.IsAlreadyExists(err) {
			wt.Expect(err).NotTo(HaveOccurred())
		}

		// Wait for the bootstrap PKI resources to be created.
		wt.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-selfsigned-issuer"}).
			Eventually().ShouldNot(BeNil())

		// Trigger a spec change → cache miss → GC runs.
		cke := &ccmv1alpha1.CoreWeaveKubernetesEngine{}
		wt.Expect(wt.Client().Get(wt.Context(),
			types.NamespacedName{Name: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName}, cke)).To(Succeed())
		cke.Spec.Dependencies.LWS.ManagementPolicy = ccmcommon.Managed
		wt.Expect(wt.Client().Update(wt.Context(), cke)).To(Succeed())

		// Wait for LWS deployment to confirm the reconcile (and GC) completed.
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "openshift-lws-operator", Namespace: "openshift-lws-operator",
		}).Eventually().Should(Not(BeNil()))

		// The protected PKI resources must survive across GC runs.
		NewWithT(t).Consistently(func() error {
			return wt.Client().Get(wt.Context(), types.NamespacedName{Name: "opendatahub-selfsigned-issuer"},
				resources.GvkToPartial(gvk.CertManagerClusterIssuer))
		}).WithTimeout(5 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())

		NewWithT(t).Consistently(func() error {
			return wt.Client().Get(wt.Context(), types.NamespacedName{Name: "opendatahub-ca", Namespace: "cert-manager"},
				resources.GvkToPartial(gvk.CertManagerCertificate))
		}).WithTimeout(5 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())

		NewWithT(t).Consistently(func() error {
			return wt.Client().Get(wt.Context(), types.NamespacedName{Name: "opendatahub-ca-issuer"},
				resources.GvkToPartial(gvk.CertManagerClusterIssuer))
		}).WithTimeout(5 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())
	})
}
