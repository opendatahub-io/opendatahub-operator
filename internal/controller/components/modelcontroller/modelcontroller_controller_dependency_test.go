package modelcontroller_test

import (
	"context"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	templatev1 "github.com/openshift/api/template/v1"
	v1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func startModelControllerController(t *testing.T, ctx context.Context) (*envt.EnvT, *testf.WithT) {
	t.Helper()
	g := NewWithT(t)

	// templatev1.Template is not in the default test scheme — add it
	// so Owns(&templatev1.Template{}) can resolve the GVK.
	s, err := scheme.New()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(templatev1.Install(s)).To(Succeed())

	et, err := envt.New(
		envt.WithScheme(s),
		envt.WithManager(ctrl.Options{
			Controller: ctrlconfig.Controller{SkipNameValidation: ptr.To(true)},
		}),
		envt.WithRegisterControllers(func(mgr ctrl.Manager) error {
			return modelcontroller.NewHandler().NewComponentReconciler(ctx, mgr)
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	// Register CRDs for non-built-in types used by Owns() so informers start.
	_, err = et.RegisterCRD(ctx, gvk.OpenshiftTemplate, "templates", "template", apiextensionsv1.NamespaceScoped)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = et.RegisterCRD(ctx, gvk.CoreosServiceMonitor, "servicemonitors", "servicemonitor", apiextensionsv1.NamespaceScoped)
	g.Expect(err).NotTo(HaveOccurred())

	// The reconciler reads OPERATOR_NAMESPACE; create it so apply-status succeeds.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "opendatahub-operator-system"}}
	g.Expect(et.Client().Create(ctx, ns)).To(Succeed())

	et.StartManager(t, ctx)

	tc, err := et.NewTestContext(ctx)
	g.Expect(err).NotTo(HaveOccurred())

	return et, tc.NewWithT(t)
}

func createModelControllerCR(t *testing.T, wt *testf.WithT, kserveState, wvaState operatorv1.ManagementState) {
	t.Helper()

	cr := &componentApi.ModelController{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.ModelControllerInstanceName},
		Spec: componentApi.ModelControllerSpec{
			Kserve: &componentApi.ModelControllerKerveSpec{
				ManagementState: kserveState,
				WVA: componentApi.WVASpec{
					ManagementState: wvaState,
				},
			},
		},
	}
	wt.Expect(wt.Client().Create(wt.Context(), cr)).Should(Succeed())
	envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), cr)
}

func TestModelControllerSubscriptionDependencyMonitoring(t *testing.T) {
	t.Run("OpenShift with both Managed and missing subscription sets condition to False", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		et, wt := startModelControllerController(t, ctx)
		t.Cleanup(cancel)

		crd, err := et.RegisterCRD(wt.Context(), gvk.Subscription,
			"subscriptions", "subscription", apiextensionsv1.NamespaceScoped)
		wt.Expect(err).NotTo(HaveOccurred())
		envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), crd)

		createModelControllerCR(t, wt, operatorv1.Managed, operatorv1.Managed)

		nn := types.NamespacedName{Name: componentApi.ModelControllerInstanceName}
		wt.Get(gvk.ModelController, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				modelcontroller.LLMDWVADependencies, metav1.ConditionFalse),
		)

		wt.Get(gvk.ModelController, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Custom Metrics Autoscaler")`,
				modelcontroller.LLMDWVADependencies),
		)
	})

	t.Run("OpenShift with both Managed and subscription present sets condition to True", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		et, wt := startModelControllerController(t, ctx)
		t.Cleanup(cancel)

		crd, err := et.RegisterCRD(wt.Context(), gvk.Subscription,
			"subscriptions", "subscription", apiextensionsv1.NamespaceScoped)
		wt.Expect(err).NotTo(HaveOccurred())
		envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), crd)

		sub := &v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{Name: modelcontroller.CMAOperatorSubscription, Namespace: "default"},
		}
		wt.Expect(wt.Client().Create(wt.Context(), sub)).To(Succeed())
		envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), sub)

		createModelControllerCR(t, wt, operatorv1.Managed, operatorv1.Managed)

		nn := types.NamespacedName{Name: componentApi.ModelControllerInstanceName}
		wt.Get(gvk.ModelController, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				modelcontroller.LLMDWVADependencies, metav1.ConditionTrue),
		)
	})

	for _, tc := range []struct {
		name        string
		kserveState operatorv1.ManagementState
		wvaState    operatorv1.ManagementState
	}{
		{"KServe Removed", operatorv1.Removed, operatorv1.Managed},
		{"WVA Removed", operatorv1.Managed, operatorv1.Removed},
	} {
		t.Run("OpenShift with "+tc.name+" skips subscription check", func(t *testing.T) {
			cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
			t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

			ctx, cancel := context.WithCancel(context.Background())
			et, wt := startModelControllerController(t, ctx)
			t.Cleanup(cancel)

			crd, err := et.RegisterCRD(wt.Context(), gvk.Subscription,
				"subscriptions", "subscription", apiextensionsv1.NamespaceScoped)
			wt.Expect(err).NotTo(HaveOccurred())
			envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), crd)

			createModelControllerCR(t, wt, tc.kserveState, tc.wvaState)

			nn := types.NamespacedName{Name: componentApi.ModelControllerInstanceName}

			wt.Get(gvk.ModelController, nn).Eventually().Should(
				jq.Match(`[.status.conditions[] | select(.type == "Ready")] | length > 0`),
			)

			wt.Get(gvk.ModelController, nn).Consistently().WithTimeout(5 * time.Second).Should(
				jq.Match(`all(.status.conditions[]?.type; . != "%s")`,
					modelcontroller.LLMDWVADependencies),
			)
		})
	}

	t.Run("Kubernetes cluster skips subscription check", func(t *testing.T) {
		cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
		t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

		ctx, cancel := context.WithCancel(context.Background())
		_, wt := startModelControllerController(t, ctx)
		t.Cleanup(cancel)

		createModelControllerCR(t, wt, operatorv1.Managed, operatorv1.Managed)

		nn := types.NamespacedName{Name: componentApi.ModelControllerInstanceName}

		wt.Get(gvk.ModelController, nn).Eventually().Should(
			jq.Match(`[.status.conditions[] | select(.type == "Ready")] | length > 0`),
		)

		wt.Get(gvk.ModelController, nn).Consistently().WithTimeout(5 * time.Second).Should(
			jq.Match(`all(.status.conditions[]?.type; . != "%s")`,
				modelcontroller.LLMDWVADependencies),
		)
	})
}
