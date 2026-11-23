package builtin_test

import (
	"fmt"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/data"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dataconnecthub"
)

func TestDataAndDCHPlatformEnablementCombinations(t *testing.T) {
	for _, featureStore := range []bool{false, true} {
		for _, dataRegistry := range []bool{false, true} {
			for _, dch := range []bool{false, true} {
				name := fmt.Sprintf("featureStore=%t/dataRegistry=%t/dataConnectHub=%t", featureStore, dataRegistry, dch)
				t.Run(name, func(t *testing.T) {
					state := func(enabled bool) operatorv1.ManagementState {
						if enabled {
							return operatorv1.Managed
						}
						return operatorv1.Removed
					}
					dsc := &dscApi.DataScienceCluster{}
					dsc.Spec.Components.Data.FeatureStore.ManagementState = state(featureStore)
					dsc.Spec.Components.Data.DataRegistry.ManagementState = state(dataRegistry)
					dsc.Spec.Components.Data.DataConnectHub.ManagementState = state(dch)
					ctx := &modules.DSCContext{DSC: dsc}

					pm := &configApi.PlatformModules{}
					feast := data.NewHandler()
					hub := dataconnecthub.NewHandler()
					feast.PopulatePlatformModule(pm, ctx)
					hub.PopulatePlatformModule(pm, ctx)

					expectedFeast := featureStore || dataRegistry
					require.Equal(t, expectedFeast, feast.IsEnabled(pm), "Feast must depend only on its own children")
					require.Equal(t, dch, hub.IsEnabled(pm), "DCH must depend only on its own capability")
					require.Equal(t, state(expectedFeast), pm.Data.ManagementState)
					require.Equal(t, state(dch), pm.DataConnectHub.ManagementState)
				})
			}
		}
	}
}
