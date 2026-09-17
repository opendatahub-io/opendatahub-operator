package dsc

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

type AIHubSuite struct {
	WebhookSuite
}

func (m AIHubSuite) run(t *testing.T) {
	t.Run("v2_v3", m.v2ToV3)
	t.Run("v3_v2", m.v3ToV2)
}

//nolint:dupl // Both directions assert the same conversion mapping through different API versions.
func (m AIHubSuite) v2ToV3(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				AIHub: componentApi.DSCAIHub{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					AIHubCommonSpec: componentApi.AIHubCommonSpec{
						ApplicationNamespace: "model-registries",
					},
				},
			},
		},
	}))

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
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
	}))
}

//nolint:dupl // Both directions assert the same conversion mapping through different API versions.
func (m AIHubSuite) v3ToV2(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				AIHub: componentApi.DSCAIHub{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					AIHubCommonSpec: componentApi.AIHubCommonSpec{
						ApplicationNamespace: "model-registries",
					},
				},
			},
		},
	}).Should(Succeed())

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
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
	}))

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				AIHub: componentApi.DSCAIHub{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					AIHubCommonSpec: componentApi.AIHubCommonSpec{
						ApplicationNamespace: "model-registries",
					},
				},
			},
		},
	}))
}
