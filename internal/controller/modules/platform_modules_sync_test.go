package modules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	modulebuiltin "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
)

func TestPlatformModulesMatchBuiltInHandlers(t *testing.T) {
	t.Parallel()

	builtIn := modulebuiltin.Names()
	builtInNames := make(map[string]bool, len(builtIn))
	for _, name := range builtIn {
		builtInNames[name] = true
	}
	platformModuleNames := (&configApi.PlatformModules{}).ModuleNames()
	platformModules := make(map[string]bool, len(platformModuleNames))
	for _, name := range platformModuleNames {
		platformModules[name] = true
	}

	for _, name := range platformModuleNames {
		assert.True(t, builtInNames[name], "Platform CR module %q has no built-in handler", name)
	}

	for _, name := range builtIn {
		assert.True(t, platformModules[name],
			"built-in handler %q has no matching Platform CR spec.modules field", name)
	}
}
