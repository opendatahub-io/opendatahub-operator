package data_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
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

		v3 := &dscv3.DataScienceCluster{}
		g.Expect(env.Client().Get(ctx, client.ObjectKeyFromObject(v2), v3)).To(Succeed())
		g.Expect(v3.Spec.Components.Data.FeatureStore.ManagementState).To(Equal(operatorv1.Removed))
		g.Expect(v3.Spec.Components.Data.DataRegistry.ManagementState).To(Equal(operatorv1.Managed))

		g.Expect(env.Client().Delete(ctx, v2)).To(Succeed())
	})

	t.Run("v3v2", func(t *testing.T) {
		g := NewWithT(t)
		v3 := &dscv3.DataScienceCluster{}
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
}
