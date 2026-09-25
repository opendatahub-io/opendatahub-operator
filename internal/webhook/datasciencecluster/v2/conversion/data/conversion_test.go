package data_test

import (
	"fmt"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"

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

				hub := &dscv3.DataScienceCluster{}
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
				source := &dscv3.DataScienceCluster{}
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
