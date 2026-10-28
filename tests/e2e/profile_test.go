package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTestProfile(t *testing.T) {
	originalOpts := testOpts
	originalServices := Services
	t.Cleanup(func() {
		testOpts = originalOpts
		Services = originalServices
	})

	reset := func() {
		testOpts = TestContextConfig{
			dependantOperatorsManagementTest: true,
			dscManagementTest:                true,
			dscValidationTest:                true,
			operatorControllerTest:           true,
			operatorResilienceTest:           true,

			conversionWebhookTest:     true,
			conversionWebhookDSC:      true,
			conversionWebhookPlatform: true,
			dagOrderingTest:           true,
			syntheticModuleTest:       true,
			v2tov3upgradeTest:         true,
			cleanUpPreviousResources:  true,
		}
		Services.enabled = true
	}

	t.Run("core excludes DAG", func(t *testing.T) {
		reset()
		require.NoError(t, applyTestProfile(ProfileCore))
		assert.False(t, testOpts.dagOrderingTest)
		assert.False(t, testOpts.dependantOperatorsManagementTest)
		assert.True(t, testOpts.syntheticModuleTest)
		assert.True(t, testOpts.dscManagementTest)
		assert.False(t, testOpts.conversionWebhookDSC)
		assert.True(t, testOpts.conversionWebhookPlatform)
		assert.True(t, Services.enabled)
	})

	t.Run("synthetic isolates fixture contract", func(t *testing.T) {
		reset()
		require.NoError(t, applyTestProfile(ProfileSynthetic))
		assert.True(t, testOpts.syntheticModuleTest)
		assert.False(t, testOpts.dagOrderingTest)
		assert.False(t, testOpts.dscManagementTest)
		assert.False(t, Services.enabled)
	})

	t.Run("dag isolates full integration", func(t *testing.T) {
		reset()
		require.NoError(t, applyTestProfile(ProfileDAG))
		assert.True(t, testOpts.dagOrderingTest)
		assert.False(t, testOpts.syntheticModuleTest)
		assert.False(t, testOpts.cleanUpPreviousResources)
		assert.False(t, Services.enabled)
	})

	t.Run("invalid profile fails", func(t *testing.T) {
		reset()
		require.Error(t, applyTestProfile("invalid"))
	})
}
