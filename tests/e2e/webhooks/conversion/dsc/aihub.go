package dsc

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

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
	t.Run("default_namespace", m.defaultNamespace)
}

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
		jq.Match(`.status.components.aiHub.instancesNamespace == "model-registries"`),
		jq.Match(`(.status.components.aiHub | has("applicationNamespace")) == false`),
	))

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.modelregistry.managementState == "Managed"`),
		jq.Match(`.spec.components.modelregistry.registriesNamespace == "model-registries"`),
		jq.Match(`.status.components.modelregistry.managementState == "Managed"`),
		jq.Match(`.status.components.modelregistry.registriesNamespace == "model-registries"`),
	))
	m.assertModuleNamespace(t, "model-registries")
}

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
		jq.Match(`.status.components.aiHub.instancesNamespace == "model-registries"`),
		jq.Match(`(.status.components.aiHub | has("applicationNamespace")) == false`),
	))
	m.assertModuleNamespace(t, "model-registries")
}

func (m AIHubSuite) defaultNamespace(t *testing.T) {
	for _, tc := range []struct {
		name      string
		gvk       schema.GroupVersionKind
		component string
	}{
		{"v2", gvk.DataScienceClusterV2, "modelregistry"},
		{"v3", gvk.DataScienceClusterV3, "aiHub"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := m.newScenario(t)
			object := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"components": map[string]any{
						tc.component: map[string]any{"managementState": "Managed"},
					},
				},
			}}
			object.SetGroupVersionKind(tc.gvk)
			object.SetName(dscKey.Name)
			w.Apply(object).Should(Succeed())

			actual := &dscApi.DataScienceCluster{}
			w.Eventually(func() error { return w.Client().Get(t.Context(), dscKey, actual) }).Should(Succeed())
			namespace := actual.Spec.Components.AIHub.InstancesNamespace
			// The test binary can run against either platform flavor.
			w.Expect(namespace).To(BeElementOf("odh-model-registries", "rhoai-model-registries"))
			m.assertModuleNamespace(t, namespace)
			w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
				jq.Match(`.status.components.aiHub.instancesNamespace == "%s"`, namespace),
				jq.Match(`.status.components.aiHub.managementState == "Managed"`),
			))
			w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
				jq.Match(`.spec.components.modelregistry.registriesNamespace == "%s"`, namespace),
				jq.Match(`.status.components.modelregistry.registriesNamespace == "%s"`, namespace),
			))
		})
	}
}

func (m AIHubSuite) assertModuleNamespace(t *testing.T, namespace string) {
	t.Helper()
	w := m.testContext.NewWithT(t)
	w.Get(gvk.AIHub, types.NamespacedName{Name: "default-aihub"}).Should(
		jq.Match(`.spec.instancesNamespace == "%s"`, namespace),
	)
}
