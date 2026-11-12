package flags_test

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

func TestSyntheticE2EModuleRequiresPositiveOptIn(t *testing.T) {
	t.Cleanup(viper.Reset)

	assert.False(t, flags.IsSyntheticE2EModuleEnabled())
	viper.Set("enable-synthetic-e2e-module", true)
	assert.True(t, flags.IsSyntheticE2EModuleEnabled())
}
