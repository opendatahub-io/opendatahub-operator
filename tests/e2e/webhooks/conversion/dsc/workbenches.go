package dsc

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

func (m WorkbenchesSuite) run(t *testing.T) {
	t.Run("v2_v3", m.v2ToV3)
	t.Run("v3_v2", m.v3ToV2)
}

//nolint:dupl // Both directions must verify the versioned Workbenches status paths.
func (m WorkbenchesSuite) v2ToV3(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchesV2: componentApi.WorkbenchesV2Spec{ManagementState: operatorv1.Managed},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.workbenches.managementState == "Managed"`),
		jq.Match(`.spec.components.workbenches.workbenchesV2.managementState == "Managed"`),
		jq.Match(`.status.components.workbenches.managementState == "Managed"`),
		jq.Match(`.status.components.workbenchesV2.managementState == "Managed"`),
	))

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.workbenches.managementState == "Managed"`),
		jq.Match(`.spec.components.workbenches.workbenchesV2.managementState == "Managed"`),
		jq.Match(`.status.components.workbenches.managementState == "Managed"`),
		jq.Match(`.status.components.workbenchesV2.managementState == "Managed"`),
	))
}

//nolint:dupl // Both directions must verify the versioned Workbenches status paths.
func (m WorkbenchesSuite) v3ToV2(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchesV2: componentApi.WorkbenchesV2Spec{ManagementState: operatorv1.Managed},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.workbenches.managementState == "Managed"`),
		jq.Match(`.spec.components.workbenches.workbenchesV2.managementState == "Managed"`),
		jq.Match(`.status.components.workbenches.managementState == "Managed"`),
		jq.Match(`.status.components.workbenchesV2.managementState == "Managed"`),
	))

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.workbenches.managementState == "Managed"`),
		jq.Match(`.spec.components.workbenches.workbenchesV2.managementState == "Managed"`),
		jq.Match(`.status.components.workbenches.managementState == "Managed"`),
		jq.Match(`.status.components.workbenchesV2.managementState == "Managed"`),
	))
}
