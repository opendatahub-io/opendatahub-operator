//go:build integration

package datasciencecluster_test

import (
	"context"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	dscctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func TestModelRegistryBootstrapWithEmptyGatewayDomain(t *testing.T) {
	g := NewWithT(t)
	g.DurationBundle.EventuallyTimeout = 30 * time.Second
	g.DurationBundle.EventuallyPollingInterval = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if cr.DefaultRegistry().Lookup(componentApi.ModelRegistryComponentName) == nil {
		cr.Add(modelregistry.NewHandler(), cr.WithRunlevel(dag.RL(20)))
	}

	et, err := envt.New(
		envt.WithManager(ctrl.Options{
			Controller: config.Controller{SkipNameValidation: new(true)},
		}),
		envt.WithRegisterControllers(func(mgr manager.Manager) error {
			return dscctrl.NewDataScienceClusterReconciler(ctx, mgr)
		}),
	)
	g.Expect(err).ToNot(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	go func() {
		_ = et.Manager().Start(ctx)
	}()

	g.Eventually(func() bool {
		return et.Manager().GetCache().WaitForCacheSync(ctx)
	}).Should(BeTrue())

	dsci := newDSCIForBootstrapTest()
	gwc := newGatewayConfigForBootstrapTest()
	dsc := newDSCForBootstrapTest()

	g.Expect(et.Client().Create(ctx, dsci)).To(Succeed())
	g.Expect(et.Client().Create(ctx, gwc)).To(Succeed())
	g.Expect(et.Client().Create(ctx, dsc)).To(Succeed())

	tc, err := testf.NewTestContext(
		testf.WithClient(et.Client()),
		testf.WithContext(ctx),
		testf.WithTOptions(
			testf.WithEventuallyTimeout(30*time.Second),
			testf.WithEventuallyPollingInterval(200*time.Millisecond),
		),
	)
	g.Expect(err).ToNot(HaveOccurred())
	tf := tc.NewWithT(t)

	modelRegistryNN := types.NamespacedName{Name: componentApi.ModelRegistryInstanceName}
	modelRegistryGVK := componentApi.GroupVersion.WithKind(componentApi.ModelRegistryKind)

	tf.Get(modelRegistryGVK, modelRegistryNN).
		Eventually().
		ShouldNot(BeNil())

	tf.Get(modelRegistryGVK, modelRegistryNN).
		Eventually().
		Should(jq.Match(`.spec.gateway == null`))

	tf.UpdateStatus(
		serviceApi.GroupVersion.WithKind(serviceApi.GatewayConfigKind),
		types.NamespacedName{Name: serviceApi.GatewayConfigName},
		testf.Transform(`.status.domain = "apps.example.com"`),
	).Eventually().Should(Succeed())

	tf.Get(modelRegistryGVK, modelRegistryNN).
		Eventually().
		Should(jq.Match(`.spec.gateway.domain == "apps.example.com"`))
}

func newDSCIForBootstrapTest() *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: dsciv2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "bootstrap-dsci",
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "opendatahub",
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: "opendatahub",
				},
			},
		},
	}
}

func newGatewayConfigForBootstrapTest() *serviceApi.GatewayConfig {
	return &serviceApi.GatewayConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       serviceApi.GatewayConfigKind,
			APIVersion: serviceApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
	}
}

func newDSCForBootstrapTest() *dscv2.DataScienceCluster {
	return &dscv2.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "bootstrap-dsc",
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				ModelRegistry: componentApi.DSCModelRegistry{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
	}
}
