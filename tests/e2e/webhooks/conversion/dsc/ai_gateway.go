package dsc

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

const (
	maasV2StateMarker = "conversion.opendatahub.io/maas-v2-state"
	legacyManaged     = "legacy-managed"
	maasValidation    = "maas-validation"
)

func (m AIGatewaySuite) run(t *testing.T) {
	t.Run("v2_v3", func(t *testing.T) {
		t.Run("submodule statuses", m.submoduleStatusesV2ToV3)
		t.Run("defaulted canonical Removed", m.defaultedCanonical)
		t.Run("explicit canonical Removed", m.explicitCanonicalRemoved)
		t.Run("legacy Removed", m.legacyRemoved)
		t.Run("canonical Managed wins", m.canonicalManaged)
		t.Run("legacy Removed with omitted KServe parent", m.legacyRemovedOmittedKserveParent)
		t.Run("canonical Managed with Managed KServe parent", m.canonicalManagedWithKserveParent)
		t.Run("canonical Managed with legacy Managed", m.canonicalManagedWithLegacyManaged)
		t.Run("KServe parent transition", m.kserveParentTransition)
		t.Run("omitted KServe parent", m.omittedKserveParent)
		t.Run("canonical retirement", m.canonicalRetirement)
		t.Run("marker preservation", m.markerPreservation)
		t.Run("readiness condition", m.maasReadinessCondition)
		t.Run("delete and recreate", m.deleteAndRecreate)
	})

	t.Run("v3_v2", func(t *testing.T) {
		t.Run("submodule statuses", m.submoduleStatusesV3ToV2)
		t.Run("canonical Managed", m.v3CanonicalManaged)
		t.Run("canonical Removed", m.v3CanonicalRemoved)
	})
}

//nolint:dupl // Both directions must verify the versioned aggregate and submodule status paths.
func (m AIGatewaySuite) submoduleStatusesV2ToV3(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{ManagementState: operatorv1.Managed},
						BatchGateway:     componentApi.AIGatewayBatchGatewaySpec{ManagementState: operatorv1.Managed},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.batchGateway.managementState == "Managed"`),
		jq.Match(`.status.components.aigateway.managementState == "Managed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.status.components.batchGateway.managementState == "Managed"`),
	))

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.batchGateway.managementState == "Managed"`),
		jq.Match(`.status.components.aigateway.managementState == "Managed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.status.components.batchGateway.managementState == "Managed"`),
	))
}

//nolint:dupl // Both directions must verify the versioned aggregate and submodule status paths.
func (m AIGatewaySuite) submoduleStatusesV3ToV2(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{ManagementState: operatorv1.Managed},
						BatchGateway:     componentApi.AIGatewayBatchGatewaySpec{ManagementState: operatorv1.Managed},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.batchGateway.managementState == "Managed"`),
		jq.Match(`.status.components.aigateway.managementState == "Managed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.status.components.batchGateway.managementState == "Managed"`),
	))

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.batchGateway.managementState == "Managed"`),
		jq.Match(`.status.components.aigateway.managementState == "Managed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.status.components.batchGateway.managementState == "Managed"`),
	))
}

// defaultedCanonical verifies that omitted canonical MaaS defaults to Removed
// while a legacy Managed value remains visible and tracked.
func (m AIGatewaySuite) defaultedCanonical(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: dscKey.Name,
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(
		And(
			jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
			matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
		),
	)
	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Removed"`),
	))
}

// explicitCanonicalRemoved verifies that explicitly setting canonical MaaS to
// Removed has the same result as relying on its default.
func (m AIGatewaySuite) explicitCanonicalRemoved(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(
		And(
			jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
			matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
		),
	)

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
	))
}

// legacyRemoved verifies that a legacy Removed value does not create a
// legacy-managed provenance marker.
func (m AIGatewaySuite) legacyRemoved(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(
		And(
			jq.Match(`.spec.components.kserve.managementState == "Managed"`),
			jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
			jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
			Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
		),
	)

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Managed"`),
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
	))
}

// canonicalManaged verifies that an explicitly managed canonical MaaS value
// wins over legacy state and does not create provenance.
func (m AIGatewaySuite) canonicalManaged(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
	),
	)

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Removed"`),
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
	))
}

// legacyRemovedOmittedKserveParent verifies that an omitted KServe parent is
// effective as Removed without adding a legacy provenance marker.
func (m AIGatewaySuite) legacyRemovedOmittedKserveParent(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
	),
	)

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
	))
}

// canonicalManagedWithKserveParent verifies that canonical and KServe parent
// Managed values remain enabled together.
func (m AIGatewaySuite) canonicalManagedWithKserveParent(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
	),
	)

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Managed"`),
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
	))
}

// canonicalManagedWithLegacyManaged verifies that canonical Managed wins when
// both canonical and legacy MaaS are Managed.
func (m AIGatewaySuite) canonicalManagedWithLegacyManaged(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
	),
	)

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Removed"`),
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
	))
}

// kserveParentTransition verifies that KServe gates legacy MaaS without
// disabling the migrated AI Gateway parent.
func (m AIGatewaySuite) kserveParentTransition(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
	))
	w.Get(gvk.DataScienceClusterV3, dscKey).Eventually().
		WithTimeout(5 * time.Minute).
		Should(And(
			jq.Match(`.status.components.modelsAsAService.managementState == "Removed"`),
		))

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
	),
	)
	w.Get(gvk.DataScienceClusterV3, dscKey).Eventually().
		WithTimeout(5 * time.Minute).
		Should(
			jq.Match(`.status.components.modelsAsAService.managementState == "Managed"`),
		)

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Managed"`),
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
	))

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
	),
	)
}

// omittedKserveParent verifies that legacy Managed intent is tracked even
// when the omitted KServe parent is effectively Removed.
func (m AIGatewaySuite) omittedKserveParent(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Managed,
					},
				}},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
	),
	)

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
	))
}

// canonicalRetirement verifies that canonical MaaS can be retired and later
// re-enabled only through the v3 field.
func (m AIGatewaySuite) canonicalRetirement(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
	))

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{AIGateway: componentApi.DSCAIGateway{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
				AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			}},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
	))

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: dscKey.Name,
			Annotations: map[string]string{
				maasV2StateMarker: legacyManaged,
			},
		},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{AIGateway: componentApi.DSCAIGateway{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
				AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			}},
		},
	}).Should(Succeed())

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{AIGateway: componentApi.DSCAIGateway{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
				AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			}},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
	))
}

// markerPreservation verifies that unrelated v3 spec and metadata changes do
// not remove legacy provenance while the canonical field remains Managed.
func (m AIGatewaySuite) markerPreservation(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
	))

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   dscKey.Name,
			Labels: map[string]string{maasValidation: "preserved"},
		},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		matchers.HaveLabel(maasValidation, "preserved"),
		matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
	))

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   dscKey.Name,
			Labels: map[string]string{maasValidation: "preserved"},
		},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{Kserve: componentApi.DSCKserve{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
			}, AIGateway: componentApi.DSCAIGateway{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
				AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			}},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		matchers.HaveLabel(maasValidation, "preserved"),
		matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
	))

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   dscKey.Name,
			Labels: map[string]string{maasValidation: "preserved"},
		},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{Kserve: componentApi.DSCKserve{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
			}, AIGateway: componentApi.DSCAIGateway{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
				AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			}},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Removed"`),
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
	))
}

// maasReadinessCondition keeps the readiness conversion scenario focused on
// the canonical spec transition; status is observed from the controller but
// not written by e2e.
func (m AIGatewaySuite) maasReadinessCondition(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
	}).Should(
		Succeed(),
	)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Removed"`),
	))

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{AIGateway: componentApi.DSCAIGateway{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
				AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			}},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Managed"`),
	))
}

// deleteAndRecreate verifies that deleting the v2 representation removes the
// converted object and that a later native v3 object starts without provenance.
func (m AIGatewaySuite) deleteAndRecreate(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
	))

	w.DeleteObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
	}).Eventually().Should(Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Eventually().Should(
		BeNil(),
	)

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{Kserve: componentApi.DSCKserve{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
			}, AIGateway: componentApi.DSCAIGateway{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
				AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			}},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Removed"`),
	))
}

//nolint:dupl // Managed and Removed cases must both exercise v3-to-v2 conversion.
func (m AIGatewaySuite) v3CanonicalManaged(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Managed"`),
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Removed"`),
	))
}

//nolint:dupl // Managed and Removed cases must both exercise v3-to-v2 conversion.
func (m AIGatewaySuite) v3CanonicalRemoved(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
		jq.Match(`.spec.components.kserve.managementState == "Removed"`),
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Removed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		jq.Match(`.status.components.modelsAsAService.managementState == "Removed"`),
	))
}
