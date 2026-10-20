package data_test

import (
	"fmt"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"

	. "github.com/onsi/gomega"
)

func TestDataConversionV2ToV3(t *testing.T) {
	states := []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed}
	for _, featureStoreState := range states {
		for _, dataRegistryState := range states {
			name := fmt.Sprintf("featureStore=%q/dataRegistry=%q", featureStoreState, dataRegistryState)
			t.Run(name, func(t *testing.T) {
				g := NewWithT(t)
				source := &dscv2.DataScienceCluster{}
				source.Spec.Components.FeastOperator.ManagementState = featureStoreState
				source.Spec.Components.FeastOperator.DataRegistry.ManagementState = dataRegistryState

				hub := &dscApi.DataScienceCluster{}
				g.Expect(source.ConvertTo(hub)).To(Succeed())

				g.Expect(hub.Spec.Components.Data.FeatureStore.ManagementState).To(Equal(featureStoreState))
				g.Expect(hub.Spec.Components.Data.DataRegistry.ManagementState).To(Equal(dataRegistryState))
			})
		}
	}
}

func TestDataConversionV3ToV2(t *testing.T) {
	states := []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed}
	for _, featureStoreState := range states {
		for _, dataRegistryState := range states {
			name := fmt.Sprintf("featureStore=%q/dataRegistry=%q", featureStoreState, dataRegistryState)
			t.Run(name, func(t *testing.T) {
				g := NewWithT(t)
				source := &dscApi.DataScienceCluster{}
				source.Spec.Components.Data.FeatureStore.ManagementState = featureStoreState
				source.Spec.Components.Data.DataRegistry.ManagementState = dataRegistryState

				destination := &dscv2.DataScienceCluster{}
				g.Expect(destination.ConvertFrom(source)).To(Succeed())

				g.Expect(destination.Spec.Components.FeastOperator.ManagementState).To(Equal(featureStoreState))
				g.Expect(destination.Spec.Components.FeastOperator.DataRegistry.ManagementState).To(Equal(dataRegistryState))
			})
		}
	}
}

func TestDataStatusConversionV3ToV2UsesFeatureStoreState(t *testing.T) {
	states := []operatorv1.ManagementState{operatorv1.Managed, operatorv1.Removed}
	for _, featureStoreState := range states {
		for _, dataRegistryState := range states {
			name := fmt.Sprintf("featureStore=%q/dataRegistry=%q", featureStoreState, dataRegistryState)
			t.Run(name, func(t *testing.T) {
				g := NewWithT(t)
				source := &dscApi.DataScienceCluster{}
				source.Spec.Components.Data.FeatureStore.ManagementState = featureStoreState
				source.Spec.Components.Data.DataRegistry.ManagementState = dataRegistryState
				source.Status.Components.Data.ManagementState = operatorv1.Removed
				if featureStoreState == operatorv1.Managed || dataRegistryState == operatorv1.Managed {
					source.Status.Components.Data.ManagementState = operatorv1.Managed
				}
				source.Status.Components.Data.DataCommonStatus = &componentApi.DataCommonStatus{
					ComponentReleaseStatus: common.ComponentReleaseStatus{
						Releases: []common.ComponentRelease{{Name: "data", Version: "1.0.0"}},
					},
				}

				destination := &dscv2.DataScienceCluster{}
				g.Expect(destination.ConvertFrom(source)).To(Succeed())

				g.Expect(destination.Status.Components.FeastOperator.ManagementState).To(Equal(featureStoreState),
					"legacy FeastOperator status must remain a Feature Store availability signal")
				g.Expect(destination.Status.Components.FeastOperator.Releases).To(Equal(
					[]common.ComponentRelease{{Name: "data", Version: "1.0.0"}},
				))
			})
		}
	}
}
