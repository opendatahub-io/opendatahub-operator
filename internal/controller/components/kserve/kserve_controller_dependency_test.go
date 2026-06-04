//nolint:testpackage
package kserve

import (
	"context"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func startKserveController(t *testing.T, ctx context.Context) (*envt.EnvT, *testf.WithT) {
	t.Helper()
	g := NewWithT(t)

	et, err := envt.New(
		envt.WithManager(ctrl.Options{
			Controller: ctrlconfig.Controller{SkipNameValidation: ptr.To(true)},
		}),
		envt.WithRegisterControllers(func(mgr ctrl.Manager) error {
			return NewHandler().NewComponentReconciler(ctx, mgr)
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	// SCC is an OpenShift-only type used by Owns() in the kserve controller.
	// Without this CRD, controller-runtime retries the informer for ~30s before starting.
	sccGVK := schema.GroupVersionKind{Group: "security.openshift.io", Version: "v1", Kind: "SecurityContextConstraints"}
	_, err = et.RegisterCRD(ctx, sccGVK, "securitycontextconstraints", "securitycontextconstraints", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-operator-ns"}}
	g.Expect(et.Client().Create(ctx, ns)).To(Succeed())

	et.StartManager(t, ctx)

	tc, err := et.NewTestContext(ctx)
	g.Expect(err).NotTo(HaveOccurred())

	return et, tc.NewWithT(t)
}

func createKserveDependencyCR(t *testing.T, wt *testf.WithT) {
	t.Helper()

	cr := &componentApi.Kserve{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.KserveInstanceName},
	}
	wt.Expect(wt.Client().Create(wt.Context(), cr)).Should(Succeed())
	envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), cr)
}

func TestCRDDependencyMonitoring(t *testing.T) {
	t.Run("Kubernetes cluster with missing CRDs sets DependenciesAvailable to False", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		_, wt := startKserveController(t, ctx)
		t.Cleanup(cancel)

		createKserveDependencyCR(t, wt)

		nn := types.NamespacedName{Name: componentApi.KserveInstanceName}
		wt.Get(gvk.Kserve, nn).Eventually().Should(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionDependenciesAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`,
				status.ConditionDependenciesAvailable, precondition.PreConditionFailedReason),
		))

		for _, crdGVK := range xksDependencyCRDs {
			wt.Get(gvk.Kserve, nn).Eventually().Should(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("%s")`,
					status.ConditionDependenciesAvailable, crdGVK.Kind),
			)
		}
	})

	t.Run("Kubernetes cluster with all CRDs present sets DependenciesAvailable to True", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		et, wt := startKserveController(t, ctx)
		t.Cleanup(cancel)

		for _, crdGVK := range xksDependencyCRDs {
			plural := strings.ToLower(crdGVK.Kind) + "s"
			singular := strings.ToLower(crdGVK.Kind)

			crd, err := et.RegisterCRD(wt.Context(), crdGVK,
				plural, singular,
				apiextensionsv1.NamespaceScoped)
			wt.Expect(err).NotTo(HaveOccurred())
			envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), crd)
		}

		createKserveDependencyCR(t, wt)

		nn := types.NamespacedName{Name: componentApi.KserveInstanceName}
		wt.Get(gvk.Kserve, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionDependenciesAvailable, metav1.ConditionTrue),
		)
	})

	t.Run("Kubernetes cluster without LWS CRD sets WideEPDependencies to False with Info severity", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		_, wt := startKserveController(t, ctx)
		t.Cleanup(cancel)

		createKserveDependencyCR(t, wt)

		nn := types.NamespacedName{Name: componentApi.KserveInstanceName}
		wt.Get(gvk.Kserve, nn).Eventually().Should(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				LLMInferenceServiceWideEPDependencies, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`,
				LLMInferenceServiceWideEPDependencies, common.ConditionSeverityInfo),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("LeaderWorkerSet")`,
				LLMInferenceServiceWideEPDependencies),
		))
	})

	t.Run("Kubernetes cluster with LWS CRD sets WideEPDependencies to True", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		et, wt := startKserveController(t, ctx)
		t.Cleanup(cancel)

		crd, err := et.RegisterCRD(wt.Context(), gvk.LeaderWorkerSetV1,
			"leaderworkersets", "leaderworkerset",
			apiextensionsv1.NamespaceScoped)
		wt.Expect(err).NotTo(HaveOccurred())
		envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), crd)

		createKserveDependencyCR(t, wt)

		nn := types.NamespacedName{Name: componentApi.KserveInstanceName}
		wt.Get(gvk.Kserve, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				LLMInferenceServiceWideEPDependencies, metav1.ConditionTrue),
		)
	})

	t.Run("OpenShift cluster skips CRD checks", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		_, wt := startKserveController(t, ctx)
		t.Cleanup(cancel)

		createKserveDependencyCR(t, wt)

		nn := types.NamespacedName{Name: componentApi.KserveInstanceName}

		// Wait for reconciliation by checking that subscription conditions exist.
		wt.Get(gvk.Kserve, nn).Eventually().Should(
			jq.Match(`[.status.conditions[] | select(.type == "%s")] | length > 0`,
				LLMInferenceServiceDependencies),
		)

		// DependenciesAvailable should not be False from CRD checks (CRD precondition skipped on OpenShift)
		wt.Get(gvk.Kserve, nn).Consistently().WithTimeout(5 * time.Second).Should(Not(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionDependenciesAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`,
				status.ConditionDependenciesAvailable, precondition.PreConditionFailedReason),
		)))
	})
}

func TestSubscriptionDependencyMonitoring(t *testing.T) {
	t.Run("OpenShift cluster with missing subscriptions sets conditions to False", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		et, wt := startKserveController(t, ctx)
		t.Cleanup(cancel)

		crd, err := et.RegisterCRD(wt.Context(), gvk.Subscription,
			"subscriptions", "subscription", apiextensionsv1.NamespaceScoped)
		wt.Expect(err).NotTo(HaveOccurred())
		envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), crd)

		createKserveDependencyCR(t, wt)

		nn := types.NamespacedName{Name: componentApi.KserveInstanceName}

		wt.Get(gvk.Kserve, nn).Eventually().Should(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				LLMInferenceServiceDependencies, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				LLMInferenceServiceWideEPDependencies, metav1.ConditionFalse),
		))

		wt.Get(gvk.Kserve, nn).Eventually().Should(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Red Hat Connectivity Link")`,
				LLMInferenceServiceDependencies),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("cert-manager operator")`,
				LLMInferenceServiceDependencies),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("; ")`,
				LLMInferenceServiceDependencies),
		))
	})

	t.Run("OpenShift cluster with all subscriptions present sets conditions to True", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		et, wt := startKserveController(t, ctx)
		t.Cleanup(cancel)

		crd, err := et.RegisterCRD(wt.Context(), gvk.Subscription,
			"subscriptions", "subscription", apiextensionsv1.NamespaceScoped)
		wt.Expect(err).NotTo(HaveOccurred())
		envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), crd)

		for _, name := range []string{RHCLOperatorSubscription, LWSOperatorSubscription, CertManagerOperatorSubscription} {
			sub := &v1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			}
			wt.Expect(wt.Client().Create(wt.Context(), sub)).To(Succeed())
			envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), sub)
		}

		createKserveDependencyCR(t, wt)

		nn := types.NamespacedName{Name: componentApi.KserveInstanceName}

		wt.Get(gvk.Kserve, nn).Eventually().Should(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				LLMInferenceServiceDependencies, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				LLMInferenceServiceWideEPDependencies, metav1.ConditionTrue),
		))
	})

	t.Run("OpenShift cluster with partial subscriptions sets mixed conditions", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		et, wt := startKserveController(t, ctx)
		t.Cleanup(cancel)

		crd, err := et.RegisterCRD(wt.Context(), gvk.Subscription,
			"subscriptions", "subscription", apiextensionsv1.NamespaceScoped)
		wt.Expect(err).NotTo(HaveOccurred())
		envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), crd)

		// Only RHCL and cert-manager present — LLMInferenceServiceDependencies should pass,
		// but LLMInferenceServiceWideEPDependencies should fail (missing LWS)
		for _, name := range []string{RHCLOperatorSubscription, CertManagerOperatorSubscription} {
			sub := &v1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			}
			wt.Expect(wt.Client().Create(wt.Context(), sub)).To(Succeed())
			envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), sub)
		}

		createKserveDependencyCR(t, wt)

		nn := types.NamespacedName{Name: componentApi.KserveInstanceName}

		wt.Get(gvk.Kserve, nn).Eventually().Should(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				LLMInferenceServiceDependencies, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				LLMInferenceServiceWideEPDependencies, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("LeaderWorkerSet")`,
				LLMInferenceServiceWideEPDependencies),
		))
	})

	t.Run("Kubernetes cluster skips subscription checks", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		_, wt := startKserveController(t, ctx)
		t.Cleanup(cancel)

		createKserveDependencyCR(t, wt)

		nn := types.NamespacedName{Name: componentApi.KserveInstanceName}

		// Wait for reconciliation to happen by checking DependenciesAvailable exists
		wt.Get(gvk.Kserve, nn).Eventually().Should(
			jq.Match(`[.status.conditions[] | select(.type == "%s")] | length > 0`,
				status.ConditionDependenciesAvailable),
		)

		// Subscription conditions should not be written on Kubernetes
		wt.Get(gvk.Kserve, nn).Consistently().WithTimeout(5 * time.Second).Should(
			jq.Match(`all(.status.conditions[]?.type; . != "%s")`,
				LLMInferenceServiceDependencies),
		)

		// LLMInferenceServiceWideEPDependencies IS expected on Kubernetes (from MonitorCRDs for LWS)
		wt.Get(gvk.Kserve, nn).Eventually().Should(
			jq.Match(`[.status.conditions[] | select(.type == "%s")] | length > 0`,
				LLMInferenceServiceWideEPDependencies),
		)
	})
}
