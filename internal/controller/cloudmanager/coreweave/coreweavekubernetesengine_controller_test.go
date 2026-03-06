package coreweave_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/coreweave/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func TestCoreWeaveKubernetesEngine(t *testing.T) {
	t.Run("deploys managed dependencies", func(t *testing.T) {
		wt := tc.NewWithT(t)

		createCoreweaveCR(t, wt, ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		})

		nn := types.NamespacedName{Name: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName}

		// Wait for reconciliation to succeed
		wt.Get(gvk.CoreWeaveKubernetesEngine, nn).Eventually().Should(
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

func createCoreweaveCR(t *testing.T, wt *testf.WithT, deps ccmcommon.Dependencies) {
	t.Helper()

	cwe := &ccmv1alpha1.CoreWeaveKubernetesEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name: ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName,
		},
		Spec: ccmv1alpha1.CoreWeaveKubernetesEngineSpec{
			Dependencies: deps,
		},
	}

	wt.Expect(wt.Client().Create(wt.Context(), cwe)).Should(Succeed())
	t.Cleanup(func() {
		_ = wt.Client().Delete(wt.Context(), cwe)
	})
}
