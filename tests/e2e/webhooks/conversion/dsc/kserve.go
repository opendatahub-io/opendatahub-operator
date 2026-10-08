package dsc

import (
	"testing"

	"github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
)

func (m KserveSuite) run(t *testing.T) {
	t.Run("v2_v3_drops_wva", m.v2ToV3DropsWVA)
	t.Run("v3_v2_projects_wva_removed", m.v3ToV2ProjectsWVAsRemoved)
	t.Run("v2_admission_rejects_wva_managed", m.v2AdmissionRejectsWVAManaged)
}

func (m KserveSuite) v2AdmissionRejectsWVAManaged(t *testing.T) {
	w := m.newScenario(t)
	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						WVA: componentApi.WVASpec{ManagementState: operatorv1.Managed},
					},
				},
			},
		},
	}).Should(
		gomega.MatchError(gomega.ContainSubstring("wva")),
	)
}

func (m KserveSuite) v2ToV3DropsWVA(t *testing.T) {
	w := m.newScenario(t)
	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						WVA: componentApi.WVASpec{ManagementState: operatorv1.Removed},
					},
				},
			},
		},
	}).Should(gomega.Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(gomega.And(
		jq.Match(`(.spec.components.kserve | has("wva")) == false`),
	))
	w.Get(gvk.DataScienceClusterV2, dscKey).Should(
		jq.Match(`.spec.components.kserve.wva.managementState == "Removed"`),
	)
}

func (m KserveSuite) v3ToV2ProjectsWVAsRemoved(t *testing.T) {
	w := m.newScenario(t)
	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
				},
			},
		},
	}).Should(gomega.Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(
		jq.Match(`.spec.components.kserve.wva.managementState == "Removed"`),
	)
	w.Get(gvk.DataScienceClusterV3, dscKey).Should(gomega.And(
		jq.Match(`(.spec.components.kserve | has("wva")) == false`),
	))
}
