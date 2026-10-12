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

func (m DataSuite) run(t *testing.T) {
	t.Run("v2_v3", m.v2ToV3)
	t.Run("v3_v2", m.v3ToV2)
	t.Run("v2_v3_both_removed", m.v2ToV3BothRemoved)
	t.Run("v3_v2_both_removed", m.v3ToV2BothRemoved)
}

func (m DataSuite) v2ToV3(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				FeastOperator: componentApi.DSCFeastOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					DataRegistry: componentApi.DSCDataRegistry{
						ManagementSpec: common.ManagementSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.data.featureStore.managementState == "Removed"`),
		jq.Match(`.spec.components.data.dataRegistry.managementState == "Managed"`),
		jq.Match(`.status.components.data.managementState == "Managed"`),
	))

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.feastoperator.managementState == "Removed"`),
		jq.Match(`.spec.components.feastoperator.dataRegistry.managementState == "Managed"`),
		jq.Match(`.status.components.feastoperator.managementState == "Managed"`),
	))
}

func (m DataSuite) v3ToV2(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Data: componentApi.DSCData{
					FeatureStore: componentApi.DSCFeatureStore{
						ManagementSpec: common.ManagementSpec{
							ManagementState: operatorv1.Removed,
						},
					},
					DataRegistry: componentApi.DSCDataRegistry{
						ManagementSpec: common.ManagementSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.feastoperator.managementState == "Removed"`),
		jq.Match(`.spec.components.feastoperator.dataRegistry.managementState == "Managed"`),
		jq.Match(`.status.components.feastoperator.managementState == "Managed"`),
	))

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.data.featureStore.managementState == "Removed"`),
		jq.Match(`.spec.components.data.dataRegistry.managementState == "Managed"`),
		jq.Match(`.status.components.data.managementState == "Managed"`),
	))
}

//nolint:dupl // Both directions must verify removed Data status and readiness mapping.
func (m DataSuite) v2ToV3BothRemoved(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				FeastOperator: componentApi.DSCFeastOperator{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
					DataRegistry: componentApi.DSCDataRegistry{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.data.featureStore.managementState == "Removed"`),
		jq.Match(`.spec.components.data.dataRegistry.managementState == "Removed"`),
		jq.Match(`.status.components.data.managementState == "Removed"`),
		jq.Match(`.status.conditions[] | select(.type == "DataReady") | .status == "False"`),
	))

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.feastoperator.managementState == "Removed"`),
		jq.Match(`.spec.components.feastoperator.dataRegistry.managementState == "Removed"`),
		jq.Match(`.status.components.feastoperator.managementState == "Removed"`),
		jq.Match(`.status.conditions[] | select(.type == "FeastOperatorReady") | .status == "False"`),
	))
}

func (m DataSuite) v3ToV2BothRemoved(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Data: componentApi.DSCData{
					FeatureStore: componentApi.DSCFeatureStore{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
					},
					DataRegistry: componentApi.DSCDataRegistry{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.feastoperator.managementState == "Removed"`),
		jq.Match(`.spec.components.feastoperator.dataRegistry.managementState == "Removed"`),
		jq.Match(`.status.components.feastoperator.managementState == "Removed"`),
		jq.Match(`.status.conditions[] | select(.type == "FeastOperatorReady") | .status == "False"`),
	))

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.data.featureStore.managementState == "Removed"`),
		jq.Match(`.spec.components.data.dataRegistry.managementState == "Removed"`),
		jq.Match(`.status.components.data.managementState == "Removed"`),
		jq.Match(`.status.conditions[] | select(.type == "DataReady") | .status == "False"`),
	))
}
