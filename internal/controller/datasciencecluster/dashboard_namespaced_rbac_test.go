//nolint:testpackage
package datasciencecluster

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"

	. "github.com/onsi/gomega"
)

func TestWatchDashboardNamespacedRBACTargets(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(dscApi.AddToScheme(scheme)).Should(Succeed())

	dsc := &dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"},
		Spec: dscApi.DataScienceClusterSpec{Components: dscApi.Components{
			Workbenches: componentApi.DSCWorkbenches{
				ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Managed},
				WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: "target-notebooks"},
			},
		}},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dsc).Build()

	requests := watchDashboardNamespacedRBACTargets(t.Context(), cli, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target-notebooks"}})
	g.Expect(requests).Should(HaveLen(1))
	g.Expect(requests[0].Name).Should(Equal("default-dsc"))

	requests = watchDashboardNamespacedRBACTargets(t.Context(), cli, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "unrelated"}})
	g.Expect(requests).Should(BeEmpty())
}
