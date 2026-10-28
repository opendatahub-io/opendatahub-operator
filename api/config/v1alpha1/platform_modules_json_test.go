package v1alpha1_test

import (
	"encoding/json"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	v1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
)

func TestPlatformModulesOmitsUnsetSyntheticE2E(t *testing.T) {
	data, err := json.Marshal(v1alpha1.PlatformModules{})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "synthetice2e")
}

func TestPlatformModulesIncludesManagedSyntheticE2E(t *testing.T) {
	data, err := json.Marshal(v1alpha1.PlatformModules{
		SyntheticE2E: common.ManagementSpec{ManagementState: operatorv1.Managed},
	})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"synthetice2e"`)
}
