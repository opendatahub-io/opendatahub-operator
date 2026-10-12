package data_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

func TestDataVersionedAdmission(t *testing.T) {
	g := NewWithT(t)
	ctx, env, teardown := envtestutil.SetupEnvAndClient(
		t,
		[]envt.RegisterWebhooksFn{v2webhook.RegisterWebhooks, v3webhook.RegisterWebhooks},
		nil,
		envtestutil.DefaultWebhookTimeout,
	)
	t.Cleanup(teardown)

	g.Expect(env.ConfigureCRDConversion(ctx, "datascienceclusters.datasciencecluster.opendatahub.io")).To(Succeed())

	t.Run("v2v3", func(t *testing.T) {
		g := NewWithT(t)
		v2 := &dscv2.DataScienceCluster{}
		v2.Name = "data-v2v3"
		v2.Spec.Components.FeastOperator.ManagementState = operatorv1.Removed
		v2.Spec.Components.FeastOperator.DataRegistry.ManagementState = operatorv1.Managed
		g.Expect(env.Client().Create(ctx, v2)).To(Succeed())

		v3 := &dscApi.DataScienceCluster{}
		g.Expect(env.Client().Get(ctx, client.ObjectKeyFromObject(v2), v3)).To(Succeed())
		g.Expect(v3.Spec.Components.Data.FeatureStore.ManagementState).To(Equal(operatorv1.Removed))
		g.Expect(v3.Spec.Components.Data.DataRegistry.ManagementState).To(Equal(operatorv1.Managed))

		g.Expect(env.Client().Delete(ctx, v2)).To(Succeed())
	})

	t.Run("v3v2", func(t *testing.T) {
		g := NewWithT(t)
		v3 := &dscApi.DataScienceCluster{}
		v3.Name = "data-v3v2"
		v3.Spec.Components.Data.FeatureStore.ManagementState = operatorv1.Managed
		v3.Spec.Components.Data.DataRegistry.ManagementState = operatorv1.Removed
		g.Expect(env.Client().Create(ctx, v3)).To(Succeed())

		v2 := &dscv2.DataScienceCluster{}
		g.Expect(env.Client().Get(ctx, client.ObjectKeyFromObject(v3), v2)).To(Succeed())
		g.Expect(v2.Spec.Components.FeastOperator.ManagementState).To(Equal(operatorv1.Managed))
		g.Expect(v2.Spec.Components.FeastOperator.DataRegistry.ManagementState).To(Equal(operatorv1.Removed))

		g.Expect(env.Client().Delete(ctx, v3)).To(Succeed())
	})

	t.Run("readiness condition mapping", func(t *testing.T) {
		g := NewWithT(t)
		v2 := &dscv2.DataScienceCluster{}
		v2.Name = "data-condition-mapping"
		g.Expect(env.Client().Create(ctx, v2)).To(Succeed())
		t.Cleanup(func() {
			g.Expect(env.Client().Delete(ctx, v2)).To(Succeed())
		})

		v2.Status.Conditions = []common.Condition{{
			Type:   "FeastOperatorReady",
			Status: metav1.ConditionTrue,
			Reason: "FeatureStoreReady",
		}}
		g.Expect(env.Client().Status().Update(ctx, v2)).To(Succeed())

		v3 := &dscApi.DataScienceCluster{}
		g.Expect(env.Client().Get(ctx, client.ObjectKeyFromObject(v2), v3)).To(Succeed())
		g.Expect(v3.Status.Conditions).To(ContainElement(HaveField("Type", "DataReady")))

		v3.Status.Conditions = []common.Condition{{
			Type:   "DataReady",
			Status: metav1.ConditionTrue,
			Reason: "DataReady",
		}}
		g.Expect(env.Client().Status().Update(ctx, v3)).To(Succeed())

		convertedV2 := &dscv2.DataScienceCluster{}
		g.Expect(env.Client().Get(ctx, client.ObjectKeyFromObject(v3), convertedV2)).To(Succeed())
		g.Expect(convertedV2.Status.Conditions).To(ContainElement(HaveField("Type", "FeastOperatorReady")))
	})
}
