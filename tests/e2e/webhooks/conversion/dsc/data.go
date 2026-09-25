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

func (m DataSuite) run(t *testing.T) {
	t.Run("v2_v3", m.v2ToV3)
	t.Run("v3_v2", m.v3ToV2)
}

func (m DataSuite) v2ToV3(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
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

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
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
		Status: dscv3.DataScienceClusterStatus{
			Components: dscv3.ComponentsStatus{
				FeastOperator: componentApi.DSCFeastOperatorStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
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
				FeastOperator: componentApi.DSCFeastOperator{
					DataRegistry: componentApi.DSCDataRegistry{
						ManagementSpec: common.ManagementSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
		Status: dscv2.DataScienceClusterStatus{
			Components: dscv2.ComponentsStatus{
				FeastOperator: componentApi.DSCFeastOperatorStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}))
}

func (m DataSuite) v3ToV2(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
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

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
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
		Status: dscv2.DataScienceClusterStatus{
			Components: dscv2.ComponentsStatus{
				FeastOperator: componentApi.DSCFeastOperatorStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
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
		Status: dscv3.DataScienceClusterStatus{
			Components: dscv3.ComponentsStatus{
				FeastOperator: componentApi.DSCFeastOperatorStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}))
}
