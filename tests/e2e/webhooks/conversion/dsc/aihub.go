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

type AIHubSuite struct {
	WebhookSuite
}

func (m AIHubSuite) run(t *testing.T) {
	t.Run("v2_v3", m.v2ToV3)
	t.Run("v3_v2", m.v3ToV2)
}

//nolint:dupl // Both directions must verify the versioned AI Hub namespace status paths.
func (m AIHubSuite) v2ToV3(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				ModelRegistry: componentApi.DSCModelRegistry{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
						RegistriesNamespace: "model-registries",
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aiHub.managementState == "Managed"`),
		jq.Match(`.spec.components.aiHub.instancesNamespace == "model-registries"`),
		jq.Match(`.status.components.aiHub.managementState == "Managed"`),
		jq.Match(`.status.components.aiHub.applicationNamespace == "model-registries"`),
	))

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.modelregistry.managementState == "Managed"`),
		jq.Match(`.spec.components.modelregistry.registriesNamespace == "model-registries"`),
		jq.Match(`.status.components.modelregistry.managementState == "Managed"`),
		jq.Match(`.status.components.modelregistry.registriesNamespace == "model-registries"`),
	))
}

//nolint:dupl // Both directions must verify the versioned AI Hub namespace status paths.
func (m AIHubSuite) v3ToV2(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				AIHub: componentApi.DSCAIHub{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					AIHubCommonSpec: componentApi.AIHubCommonSpec{
						InstancesNamespace: "model-registries",
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.modelregistry.managementState == "Managed"`),
		jq.Match(`.spec.components.modelregistry.registriesNamespace == "model-registries"`),
		jq.Match(`.status.components.modelregistry.managementState == "Managed"`),
		jq.Match(`.status.components.modelregistry.registriesNamespace == "model-registries"`),
	))

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aiHub.managementState == "Managed"`),
		jq.Match(`.spec.components.aiHub.instancesNamespace == "model-registries"`),
		jq.Match(`.status.components.aiHub.managementState == "Managed"`),
		jq.Match(`.status.components.aiHub.applicationNamespace == "model-registries"`),
	))
}
