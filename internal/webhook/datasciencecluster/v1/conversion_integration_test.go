package v1_test

import (
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	v1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v1"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	dsciv1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v1"
	dsciv2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// Portal status is v2-only, so conversion from v1 must initialize it to Removed.
// Leaving it empty can make a subsequent v2 server-side status apply produce null
// for the portal status object and reject the entire status update.
func TestDataScienceClusterV1PatchAllowsV2StatusUpdate(t *testing.T) {
	t.Parallel()

	for _, state := range []operatorv1.ManagementState{operatorv1.Managed, operatorv1.Removed} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)
			ctx, env, teardown := envtestutil.SetupEnvAndClient(
				t,
				[]envt.RegisterWebhooksFn{
					v1webhook.RegisterWebhooks,
					dsciv1webhook.RegisterWebhooks,
					v2webhook.RegisterWebhooks,
					dsciv2webhook.RegisterWebhooks,
				},
				nil,
				envtestutil.DefaultWebhookTimeout,
			)
			t.Cleanup(teardown)
			k8sClient := env.Client()
			createDSCIV1(g, ctx, k8sClient)

			dsc := envtestutil.NewDSC("portal-status-"+strings.ToLower(string(state)), envtestutil.WithAllV2OnlyComponentsRemoved())
			dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Removed
			dsc.Spec.Components.Dashboard.MaaSConsumerPortal.ManagementState = state
			dsc.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed
			g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed())
			key := client.ObjectKeyFromObject(dsc)

			dsc.Status.Components.MaaSConsumerPortal.ManagementState = state
			dsc.Status.Conditions = []common.Condition{
				{Type: "AIPipelinesReady", Status: metav1.ConditionFalse, Reason: "NotReady", LastTransitionTime: metav1.Now()},
			}
			g.Expect(resources.ApplyStatus(ctx, k8sClient, dsc,
				client.FieldOwner("datasciencecluster"), client.ForceOwnership)).To(Succeed())

			spoke := &dscv1.DataScienceCluster{}
			g.Expect(k8sClient.Get(ctx, key, spoke)).To(Succeed())
			original := spoke.DeepCopy()
			spoke.Spec.Components.DataSciencePipelines.ArgoWorkflowsControllers = &componentApi.ArgoWorkflowsControllersSpec{
				ManagementState: operatorv1.Removed,
			}
			g.Expect(k8sClient.Patch(ctx, spoke, client.MergeFrom(original))).To(Succeed())
			g.Expect(k8sClient.Get(ctx, key, dsc)).To(Succeed())
			g.Expect(dsc.Spec.Components.AIPipelines.ArgoWorkflowsControllers.ManagementState).To(Equal(operatorv1.Removed))
			g.Expect(dsc.Status.Components.MaaSConsumerPortal.ManagementState).To(Equal(operatorv1.Removed))

			// The v2 controller can restore observed status using its normal status writer.
			dsc.Status.Components.MaaSConsumerPortal.ManagementState = state
			dsc.Status.Conditions[0].Status = metav1.ConditionTrue
			dsc.Status.Conditions[0].Reason = "Ready"
			g.Expect(resources.ApplyStatus(ctx, k8sClient, dsc,
				client.FieldOwner("datasciencecluster"), client.ForceOwnership)).To(Succeed())
			g.Expect(k8sClient.Get(ctx, key, dsc)).To(Succeed())
			g.Expect(dsc.Status.Components.MaaSConsumerPortal.ManagementState).To(Equal(state))

			g.Expect(k8sClient.Get(ctx, key, spoke)).To(Succeed())
			g.Expect(spoke.Status.Conditions).To(ContainElement(And(
				HaveField("Type", "DataSciencePipelinesReady"),
				HaveField("Status", metav1.ConditionTrue),
			)))

			// A v1 status write also defaults portal status without blocking later v2 writes.
			g.Expect(k8sClient.Status().Update(ctx, spoke)).To(Succeed())
			g.Expect(k8sClient.Get(ctx, key, dsc)).To(Succeed())
			g.Expect(dsc.Status.Components.MaaSConsumerPortal.ManagementState).To(Equal(operatorv1.Removed))
			dsc.Status.Components.MaaSConsumerPortal.ManagementState = state
			g.Expect(resources.ApplyStatus(ctx, k8sClient, dsc,
				client.FieldOwner("datasciencecluster"), client.ForceOwnership)).To(Succeed())
			g.Expect(k8sClient.Get(ctx, key, dsc)).To(Succeed())
			g.Expect(dsc.Status.Components.MaaSConsumerPortal.ManagementState).To(Equal(state))
		})
	}
}
