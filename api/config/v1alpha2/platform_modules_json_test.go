package v1alpha2_test

import (
	"encoding/json"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	v1alpha2 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
)

func TestPlatformModulesOmitsUnsetSyntheticE2E(t *testing.T) {
	data, err := json.Marshal(v1alpha2.PlatformModules{})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "synthetice2e")
}

func TestPlatformModulesIncludesManagedSyntheticE2E(t *testing.T) {
	data, err := json.Marshal(v1alpha2.PlatformModules{
		SyntheticE2E: common.ManagementSpec{ManagementState: operatorv1.Managed},
	})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"synthetice2e"`)
}
