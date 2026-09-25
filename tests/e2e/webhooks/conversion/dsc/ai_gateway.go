package dsc

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

const (
	maasV2StateMarker = "conversion.opendatahub.io/maas-v2-state"
	legacyManaged     = "legacy-managed"
	maasValidation    = "maas-validation"
)

func (m AIGatewaySuite) run(t *testing.T) {
	t.Run("v2_v3", func(t *testing.T) {
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
		t.Run("canonical Managed", m.v3CanonicalManaged)
		t.Run("canonical Removed", m.v3CanonicalRemoved)
	})
}

// defaultedCanonical verifies that omitted canonical MaaS defaults to Removed
// while a legacy Managed value remains visible and tracked.
func (m AIGatewaySuite) defaultedCanonical(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookSuiteDSCName,
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: webhookSuiteDSCName,
					Annotations: map[string]string{
						maasV2StateMarker: legacyManaged,
					},
				},
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
						AIGateway: componentApi.DSCAIGateway{
							AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
								ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
									ManagementState: operatorv1.Removed,
								},
							},
						},
					},
				},
			}),
			matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
		),
	)
	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Managed,
					},
				}},
				AIGateway: componentApi.DSCAIGateway{AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
			},
		},
		Status: dscv2.DataScienceClusterStatus{
			Components: dscv2.ComponentsStatus{
				ModelsAsAService: componentApi.DSCModelsAsServiceStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}))
}

// explicitCanonicalRemoved verifies that explicitly setting canonical MaaS to
// Removed has the same result as relying on its default.
func (m AIGatewaySuite) explicitCanonicalRemoved(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: webhookSuiteDSCName,
					Annotations: map[string]string{
						maasV2StateMarker: legacyManaged,
					},
				},
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
						AIGateway: componentApi.DSCAIGateway{
							AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
								ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
									ManagementState: operatorv1.Removed,
								},
							},
						},
					},
				},
			}),
			matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
		),
	)

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Managed,
					},
				}},
				AIGateway: componentApi.DSCAIGateway{AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
			},
		},
	}))
}

// legacyRemoved verifies that a legacy Removed value does not create a
// legacy-managed provenance marker.
func (m AIGatewaySuite) legacyRemoved(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
						Kserve: dscv3.DSCKserve{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Managed,
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
			}),
			Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
		),
	)

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
	}))
}

// canonicalManaged verifies that an explicitly managed canonical MaaS value
// wins over legacy state and does not create provenance.
func (m AIGatewaySuite) canonicalManaged(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
						AIGateway: componentApi.DSCAIGateway{
							AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
								ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
									ManagementState: operatorv1.Managed,
								},
							},
						},
					},
				},
			}),
			Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
		),
	)

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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
	}))
}

// legacyRemovedOmittedKserveParent verifies that an omitted KServe parent is
// effective as Removed without adding a legacy provenance marker.
func (m AIGatewaySuite) legacyRemovedOmittedKserveParent(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
			},
		},
	}).Should(
		Succeed(),
	)

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
						AIGateway: componentApi.DSCAIGateway{
							AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
								ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
									ManagementState: operatorv1.Removed,
								},
							},
						},
					},
				},
			}),
			Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
		),
	)

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
				AIGateway: componentApi.DSCAIGateway{AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
			},
		},
	}))
}

// canonicalManagedWithKserveParent verifies that canonical and KServe parent
// Managed values remain enabled together.
func (m AIGatewaySuite) canonicalManagedWithKserveParent(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
						Kserve: dscv3.DSCKserve{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Managed,
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
			}),
			Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
		),
	)

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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
	}))
}

// canonicalManagedWithLegacyManaged verifies that canonical Managed wins when
// both canonical and legacy MaaS are Managed.
func (m AIGatewaySuite) canonicalManagedWithLegacyManaged(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
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
			}),
			Not(matchers.HaveAnnotation(maasV2StateMarker, legacyManaged)),
		),
	)

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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
	}))
}

// kserveParentTransition verifies that KServe gates legacy MaaS without
// disabling the migrated AI Gateway parent.
func (m AIGatewaySuite) kserveParentTransition(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookSuiteDSCName,
			Annotations: map[string]string{
				maasV2StateMarker: legacyManaged,
			},
		},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				AIGateway: componentApi.DSCAIGateway{
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
	}))
	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Eventually().WithTimeout(5 * time.Minute).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		Status: dscv3.DataScienceClusterStatus{Components: dscv3.ComponentsStatus{
			ModelsAsAService: componentApi.DSCModelsAsServiceStatus{
				ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
			},
		}},
	}))

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: webhookSuiteDSCName,
					Annotations: map[string]string{
						maasV2StateMarker: legacyManaged,
					},
				},
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
						Kserve: dscv3.DSCKserve{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Managed,
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
			}),
			matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
		),
	)
	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Eventually().WithTimeout(5 * time.Minute).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		Status: dscv3.DataScienceClusterStatus{Components: dscv3.ComponentsStatus{
			ModelsAsAService: componentApi.DSCModelsAsServiceStatus{
				ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
			},
		}},
	}))

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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
	}))

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
						Kserve: dscv3.DSCKserve{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Removed,
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
			}),
			matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
		),
	)
}

// omittedKserveParent verifies that legacy Managed intent is tracked even
// when the omitted KServe parent is effectively Removed.
func (m AIGatewaySuite) omittedKserveParent(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Managed,
					},
				}},
			},
		},
	}).Should(
		Succeed(),
	)

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: webhookSuiteDSCName,
					Annotations: map[string]string{
						maasV2StateMarker: legacyManaged,
					},
				},
				Spec: dscv3.DataScienceClusterSpec{
					Components: dscv3.Components{
						AIGateway: componentApi.DSCAIGateway{
							AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
								ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
									ManagementState: operatorv1.Removed,
								},
							},
						},
					},
				},
			}),
			matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
		),
	)

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Managed,
					},
				}},
				AIGateway: componentApi.DSCAIGateway{AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
			},
		},
	}))
}

// canonicalRetirement verifies that canonical MaaS can be retired and later
// re-enabled only through the v3 field.
func (m AIGatewaySuite) canonicalRetirement(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookSuiteDSCName,
			Annotations: map[string]string{
				maasV2StateMarker: legacyManaged,
			},
		},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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
	}))

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
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
	}))

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookSuiteDSCName,
			Annotations: map[string]string{
				maasV2StateMarker: legacyManaged,
			},
		},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
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
	}))
}

// markerPreservation verifies that unrelated v3 spec and metadata changes do
// not remove legacy provenance while the canonical field remains Managed.
func (m AIGatewaySuite) markerPreservation(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookSuiteDSCName,
			Annotations: map[string]string{
				maasV2StateMarker: legacyManaged,
			},
		},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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
	}))

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				Kserve: dscv3.DSCKserve{
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

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   webhookSuiteDSCName,
			Labels: map[string]string{maasValidation: "preserved"},
		},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				Kserve: dscv3.DSCKserve{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(
		And(
			matchers.MatchPartialObject(&dscv3.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   webhookSuiteDSCName,
					Labels: map[string]string{maasValidation: "preserved"},
					Annotations: map[string]string{
						maasV2StateMarker: legacyManaged,
					},
				},
			}),
			matchers.HaveAnnotation(maasV2StateMarker, legacyManaged),
		),
	)

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   webhookSuiteDSCName,
			Labels: map[string]string{maasValidation: "preserved"},
		},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{Kserve: dscv3.DSCKserve{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   webhookSuiteDSCName,
			Labels: map[string]string{maasValidation: "preserved"},
			Annotations: map[string]string{
				maasV2StateMarker: legacyManaged,
			},
		},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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
	}))

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   webhookSuiteDSCName,
			Labels: map[string]string{maasValidation: "preserved"},
		},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{Kserve: dscv3.DSCKserve{
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

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
	}))
}

// maasReadinessCondition keeps the readiness conversion scenario focused on
// the canonical spec transition; status is observed from the controller but
// not written by e2e.
func (m AIGatewaySuite) maasReadinessCondition(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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
		Status: dscv3.DataScienceClusterStatus{
			Components: dscv3.ComponentsStatus{
				ModelsAsAService: componentApi.DSCModelsAsServiceStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}))

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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
		Status: dscv3.DataScienceClusterStatus{
			Components: dscv3.ComponentsStatus{
				ModelsAsAService: componentApi.DSCModelsAsServiceStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
	}))
}

// deleteAndRecreate verifies that deleting the v2 representation removes the
// converted object and that a later native v3 object starts without provenance.
func (m AIGatewaySuite) deleteAndRecreate(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookSuiteDSCName,
			Annotations: map[string]string{
				maasV2StateMarker: legacyManaged,
			},
		},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{AIGateway: componentApi.DSCAIGateway{
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
	}))

	w.DeleteObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Eventually().Should(
		MatchError(k8serr.IsNotFound, "IsNotFound"),
	)

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Eventually().Should(
		BeNil(),
	)

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{Kserve: dscv3.DSCKserve{
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

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{KserveCommonSpec: componentApi.KserveCommonSpec{
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
				AIGateway: componentApi.DSCAIGateway{AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
					ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
						ManagementState: operatorv1.Removed,
					},
				}},
			},
		},
		Status: dscv2.DataScienceClusterStatus{
			Components: dscv2.ComponentsStatus{
				ModelsAsAService: componentApi.DSCModelsAsServiceStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}))
}

//nolint:dupl // Managed and Removed cases must both exercise v3-to-v2 conversion.
func (m AIGatewaySuite) v3CanonicalManaged(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				Kserve: dscv3.DSCKserve{
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

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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
		Status: dscv2.DataScienceClusterStatus{
			Components: dscv2.ComponentsStatus{
				ModelsAsAService: componentApi.DSCModelsAsServiceStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}))
}

//nolint:dupl // Managed and Removed cases must both exercise v3-to-v2 conversion.
func (m AIGatewaySuite) v3CanonicalRemoved(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				Kserve: dscv3.DSCKserve{
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

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
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
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
		Status: dscv2.DataScienceClusterStatus{
			Components: dscv2.ComponentsStatus{
				ModelsAsAService: componentApi.DSCModelsAsServiceStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}))
}
