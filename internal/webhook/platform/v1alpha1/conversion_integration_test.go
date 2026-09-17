package v1alpha1_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	configv1alpha2 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	platformwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/platform/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

func TestPlatformVersionedConversion(t *testing.T) {
	g := NewWithT(t)
	ctx, env, teardown := envtestutil.SetupEnvAndClient(t,
		[]envt.RegisterWebhooksFn{platformwebhook.RegisterWebhooks},
		nil,
		envtestutil.DefaultWebhookTimeout,
	)
	t.Cleanup(teardown)
	g.Expect(env.ConfigureCRDConversion(ctx, "platforms.config.opendatahub.io")).To(Succeed())

	cli := env.Client()
	key := types.NamespacedName{Name: configv1alpha1.PlatformInstanceName}
	legacy := &configv1alpha1.Platform{
		ObjectMeta: metav1.ObjectMeta{
			Name:        key.Name,
			Labels:      map[string]string{"conversion": "preserved"},
			Annotations: map[string]string{"conversion": "preserved"},
		},
		Spec: configv1alpha1.PlatformSpec{Modules: configv1alpha1.PlatformModules{
			ModelRegistry: common.ManagementSpec{ManagementState: operatorv1.Managed},
			FeastOperator: common.ManagementSpec{ManagementState: operatorv1.Removed},
			Dashboard:     common.ManagementSpec{ManagementState: operatorv1.Managed},
		}},
	}
	g.Expect(cli.Create(ctx, legacy)).To(Succeed())

	hub := &configv1alpha2.Platform{}
	g.Eventually(func() error { return cli.Get(ctx, key, hub) }).Should(Succeed())
	g.Expect(hub.Labels).To(HaveKeyWithValue("conversion", "preserved"))
	g.Expect(hub.Annotations).To(HaveKeyWithValue("conversion", "preserved"))
	g.Expect(hub.Spec.Modules.AIHub.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(hub.Spec.Modules.Data.ManagementState).To(Equal(operatorv1.Removed))
	g.Expect(hub.Spec.Modules.Dashboard.ManagementState).To(Equal(operatorv1.Managed))

	hub.Spec.Modules.AIHub.ManagementState = operatorv1.Removed
	hub.Spec.Modules.Data.ManagementState = operatorv1.Managed
	g.Expect(cli.Update(ctx, hub)).To(Succeed())

	converted := &configv1alpha1.Platform{}
	g.Eventually(func() error { return cli.Get(ctx, key, converted) }).Should(Succeed())
	g.Expect(converted.Spec.Modules.ModelRegistry.ManagementState).To(Equal(operatorv1.Removed))
	g.Expect(converted.Spec.Modules.FeastOperator.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(converted.Spec.Modules.Dashboard.ManagementState).To(Equal(operatorv1.Managed))
}
