package azure_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func TestAzureKubernetesEngine(t *testing.T) {
	t.Run("deploys managed dependencies", func(t *testing.T) {
		wt := tc.NewWithT(t)

		createAzureCR(t, wt, ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		})

		nn := types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}

		// Wait for reconciliation to succeed
		wt.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)

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
}

func createAzureCR(t *testing.T, wt *testf.WithT, deps ccmcommon.Dependencies) {
	t.Helper()

	ake := &ccmv1alpha1.AzureKubernetesEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name: ccmv1alpha1.AzureKubernetesEngineInstanceName,
		},
		Spec: ccmv1alpha1.AzureKubernetesEngineSpec{
			Dependencies: deps,
		},
	}

	wt.Expect(wt.Client().Create(wt.Context(), ake)).Should(Succeed())
	t.Cleanup(func() {
		_ = wt.Client().Delete(wt.Context(), ake)
	})
}
