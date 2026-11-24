package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClearDAGReportForAttempt(t *testing.T) {
	t.Run("DAG profile removes a stale report", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "artifacts with spaces", "dag report.json")
		require.NoError(t, os.MkdirAll(filepath.Dir(reportPath), 0o750))
		require.NoError(t, os.WriteFile(reportPath, []byte("stale"), 0o600))

		require.NoError(t, clearDAGReportForAttempt(ProfileDAG, reportPath))
		assert.NoFileExists(t, reportPath)
	})

	t.Run("DAG profile accepts a missing report", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "missing.json")
		require.NoError(t, clearDAGReportForAttempt(ProfileDAG, reportPath))
	})

	t.Run("non-DAG profile preserves the report", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "dag-report.json")
		require.NoError(t, os.WriteFile(reportPath, []byte("current"), 0o600))

		require.NoError(t, clearDAGReportForAttempt(ProfileCore, reportPath))
		assert.FileExists(t, reportPath)
	})

	t.Run("empty path is ignored", func(t *testing.T) {
		require.NoError(t, clearDAGReportForAttempt(ProfileDAG, ""))
	})

	t.Run("removal errors are returned", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "non-empty-report-directory")
		require.NoError(t, os.Mkdir(reportPath, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(reportPath, "report.json"), []byte("stale"), 0o600))

		err := clearDAGReportForAttempt(ProfileDAG, reportPath)
		require.Error(t, err)
		assert.ErrorContains(t, err, "clear stale DAG report")
	})
}

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
		assert.True(t, testOpts.dependantOperatorsManagementTest)
		assert.False(t, testOpts.syntheticModuleTest)
		assert.False(t, testOpts.cleanUpPreviousResources)
		assert.False(t, Services.enabled)
	})

	t.Run("invalid profile fails", func(t *testing.T) {
		reset()
		require.Error(t, applyTestProfile("invalid"))
	})
}
