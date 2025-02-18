//nolint:testpackage
package monitoring

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

//nolint:maintidx
func TestUpdatePrometheusConfiguration(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	cli := envTest.Client()

	content, err := envTest.ReadFile("config/monitoring/prometheus/apps/prometheus-configs.yaml")
	g.Expect(err).ShouldNot(HaveOccurred())

	decoder := serializer.NewCodecFactory(envTest.Scheme()).UniversalDeserializer()
	res, err := resources.Decode(decoder, content)
	g.Expect(err).ShouldNot(HaveOccurred())

	dsc := dscv1.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"},
	}

	err = cli.Create(ctx, &dsc)
	g.Expect(err).ShouldNot(HaveOccurred())

	d := componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName},
	}

	m := componentApi.ModelRegistry{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.ModelRegistryInstanceName},
	}

	err = cli.Create(ctx, &m)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, &d)
	g.Expect(err).ShouldNot(HaveOccurred())

	// set the namespace
	res[0].SetNamespace(xid.New().String())

	ns := corev1.Namespace{}
	ns.Name = res[0].GetNamespace()

	err = cli.Create(ctx, &ns)
	g.Expect(err).ShouldNot(HaveOccurred())

	refcm := corev1.ConfigMap{}
	err = envTest.Scheme().Convert(&res[0], &refcm, ctx)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Note: the following tests should eb executed sequentially

	t.Run("None of the components is managed", func(t *testing.T) {
		g := NewWithT(t)

		c1 := mocks.MockComponentHandler{}
		c1.On("GetName").Return(componentApi.DashboardComponentName)
		c1.On("GetManagementState", mock.Anything).Return(operatorv1.Removed)

		c2 := mocks.MockComponentHandler{}
		c2.On("GetName").Return(componentApi.ModelRegistryComponentName)
		c2.On("GetManagementState", mock.Anything).Return(operatorv1.Removed)

		r := componentsregistry.Registry{}
		r.Add(&c1)
		r.Add(&c2)

		cm := refcm.DeepCopy()
		rr := types.ReconciliationRequest{Client: cli}

		err = rr.AddResources(cm)
		g.Expect(err).ShouldNot(HaveOccurred())

		a := NewUpdatePrometheusConfigAction(WithComponentsRegistry(&r))
		err = a(ctx, &types.ReconciliationRequest{})
		g.Expect(err).ShouldNot(HaveOccurred())

		ok, err := rr.GetResource(cm)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ok).Should(BeTrue())

		c := PrometheusConfig{}
		err = resources.ExtractContent(cm, prometheusConfigurationEntry, &c)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(c.RuleFiles).Should(And(
			HaveLen(2),
			Not(ContainElements(
				componentRules[componentApi.DashboardComponentName],
				componentRules[componentApi.ModelRegistryComponentName],
			))),
		)

		g.Expect(maps.Keys(cm.Data)).Should(ContainElements(maps.Keys(refcm.Data)))

		err = cli.Create(ctx, cm)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("One of the components is managed but not ready", func(t *testing.T) {
		g := NewWithT(t)

		m.Status = componentApi.ModelRegistryStatus{
			Status: common.Status{
				Conditions: []common.Condition{{
					Type:   status.ConditionTypeReady,
					Status: metav1.ConditionFalse,
				}},
			},
		}

		err = cli.Status().Update(ctx, &m)
		g.Expect(err).ShouldNot(HaveOccurred())

		c1 := mocks.MockComponentHandler{}
		c1.On("GetName").Return(componentApi.DashboardComponentName)
		c1.On("GetManagementState", mock.Anything).Return(operatorv1.Removed)

		c2 := mocks.MockComponentHandler{}
		c2.On("GetName").Return(componentApi.ModelRegistryComponentName)
		c2.On("GetManagementState", mock.Anything).Return(operatorv1.Managed)
		c2.On("NewCRObject", mock.Anything).Return(m.DeepCopy())

		r := componentsregistry.Registry{}
		r.Add(&c1)
		r.Add(&c2)

		cm := refcm.DeepCopy()
		rr := types.ReconciliationRequest{Client: cli}

		err = rr.AddResources(cm)
		g.Expect(err).ShouldNot(HaveOccurred())

		err := NewUpdatePrometheusConfigAction(WithComponentsRegistry(&r))(ctx, &rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		ok, err := rr.GetResource(cm)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ok).Should(BeTrue())

		c := PrometheusConfig{}
		err = resources.ExtractContent(cm, prometheusConfigurationEntry, &c)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(c.RuleFiles).Should(And(
			HaveLen(2),
			Not(ContainElements(
				componentRules[componentApi.DashboardComponentName],
				componentRules[componentApi.ModelRegistryComponentName],
			))),
		)

		g.Expect(maps.Keys(cm.Data)).Should(ContainElements(maps.Keys(refcm.Data)))

		err = cli.Update(ctx, cm)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("One of the components is managed and ready", func(t *testing.T) {
		g := NewWithT(t)

		m.Status = componentApi.ModelRegistryStatus{
			Status: common.Status{
				Conditions: []common.Condition{{
					Type:   status.ConditionTypeReady,
					Status: metav1.ConditionTrue,
				}},
			},
		}

		err = cli.Status().Update(ctx, &m)
		g.Expect(err).ShouldNot(HaveOccurred())

		c1 := mocks.MockComponentHandler{}
		c1.On("GetName").Return(componentApi.DashboardComponentName)
		c1.On("GetManagementState", mock.Anything).Return(operatorv1.Removed)
		// c1.On("NewCRObject", mock.Anything).Return(d.DeepCopy())

		c2 := mocks.MockComponentHandler{}
		c2.On("GetName").Return(componentApi.ModelRegistryComponentName)
		c2.On("GetManagementState", mock.Anything).Return(operatorv1.Managed)
		c2.On("NewCRObject", mock.Anything).Return(m.DeepCopy())

		r := componentsregistry.Registry{}
		r.Add(&c1)
		r.Add(&c2)

		cm := refcm.DeepCopy()
		rr := types.ReconciliationRequest{Client: cli}

		err = rr.AddResources(cm)
		g.Expect(err).ShouldNot(HaveOccurred())

		err := NewUpdatePrometheusConfigAction(WithComponentsRegistry(&r))(ctx, &rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		ok, err := rr.GetResource(cm)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ok).Should(BeTrue())

		c := PrometheusConfig{}
		err = resources.ExtractContent(cm, prometheusConfigurationEntry, &c)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(c.RuleFiles).Should(And(
			HaveLen(3),
			Not(ContainElement(componentRules[componentApi.DashboardComponentName])),
			ContainElement(componentRules[componentApi.ModelRegistryComponentName])),
		)

		g.Expect(maps.Keys(cm.Data)).Should(ContainElements(maps.Keys(refcm.Data)))

		err = cli.Update(ctx, cm)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("One of the components is managed but not ready", func(t *testing.T) {
		g := NewWithT(t)

		m.Status = componentApi.ModelRegistryStatus{
			Status: common.Status{
				Conditions: []common.Condition{{
					Type:   status.ConditionTypeReady,
					Status: metav1.ConditionFalse,
				}},
			},
		}

		err = cli.Status().Update(ctx, &m)
		g.Expect(err).ShouldNot(HaveOccurred())

		c1 := mocks.MockComponentHandler{}
		c1.On("GetName").Return(componentApi.DashboardComponentName)
		c1.On("GetManagementState", mock.Anything).Return(operatorv1.Removed)

		c2 := mocks.MockComponentHandler{}
		c2.On("GetName").Return(componentApi.ModelRegistryComponentName)
		c2.On("GetManagementState", mock.Anything).Return(operatorv1.Managed)
		c2.On("NewCRObject", mock.Anything).Return(m.DeepCopy())

		r := componentsregistry.Registry{}
		r.Add(&c1)
		r.Add(&c2)

		cm := refcm.DeepCopy()
		rr := types.ReconciliationRequest{Client: cli}

		err = rr.AddResources(cm)
		g.Expect(err).ShouldNot(HaveOccurred())

		cx := corev1.ConfigMap{}
		err = cli.Get(ctx, client.ObjectKeyFromObject(cm), &cx)
		g.Expect(err).ShouldNot(HaveOccurred())

		err := NewUpdatePrometheusConfigAction(WithComponentsRegistry(&r))(ctx, &rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		ok, err := rr.GetResource(cm)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ok).Should(BeTrue())

		c := PrometheusConfig{}
		err = resources.ExtractContent(cm, prometheusConfigurationEntry, &c)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(c.RuleFiles).Should(And(
			HaveLen(3),
			Not(ContainElement(componentRules[componentApi.DashboardComponentName])),
			ContainElement(componentRules[componentApi.ModelRegistryComponentName])),
		)

		g.Expect(maps.Keys(cm.Data)).Should(ContainElements(maps.Keys(refcm.Data)))

		err = cli.Update(ctx, cm)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("All the components are set to unmanaged", func(t *testing.T) {
		g := NewWithT(t)

		err = cli.Status().Update(ctx, &m)
		g.Expect(err).ShouldNot(HaveOccurred())

		c1 := mocks.MockComponentHandler{}
		c1.On("GetName").Return(componentApi.DashboardComponentName)
		c1.On("GetManagementState", mock.Anything).Return(operatorv1.Removed)

		c2 := mocks.MockComponentHandler{}
		c2.On("GetName").Return(componentApi.ModelRegistryComponentName)
		c2.On("GetManagementState", mock.Anything).Return(operatorv1.Removed)

		r := componentsregistry.Registry{}
		r.Add(&c1)
		r.Add(&c2)

		cm := refcm.DeepCopy()
		rr := types.ReconciliationRequest{Client: cli}

		err = rr.AddResources(cm)
		g.Expect(err).ShouldNot(HaveOccurred())

		err := NewUpdatePrometheusConfigAction(WithComponentsRegistry(&r))(ctx, &rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		ok, err := rr.GetResource(cm)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ok).Should(BeTrue())

		c := PrometheusConfig{}
		err = resources.ExtractContent(cm, prometheusConfigurationEntry, &c)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(c.RuleFiles).Should(And(
			HaveLen(2),
			Not(ContainElements(
				componentRules[componentApi.DashboardComponentName],
				componentRules[componentApi.ModelRegistryComponentName],
			))),
		)

		g.Expect(maps.Keys(cm.Data)).Should(ContainElements(maps.Keys(refcm.Data)))

		err = cli.Update(ctx, cm)
		g.Expect(err).ShouldNot(HaveOccurred())
	})
}
