package modules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	modulebuiltin "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
)

func TestPlatformModulesMatchBuiltInHandlers(t *testing.T) {
	t.Parallel()

	builtIn := modulebuiltin.Handlers()
	platformModuleNames := (configv1alpha1.PlatformModules{}).ModuleNames()
	platformModules := make(map[string]bool, len(platformModuleNames))
	for _, name := range platformModuleNames {
		platformModules[name] = true
	}

	for _, name := range platformModuleNames {
		if name == serviceApi.MonitoringServiceName {
			continue // not registered yet; see commented entry in builtin.entries()
		}
		_, ok := builtIn[name]
		assert.True(t, ok, "Platform CR module %q has no built-in handler", name)
	}

	for name := range builtIn {
		assert.True(t, platformModules[name],
			"built-in handler %q has no matching Platform CR spec.modules field", name)
	}
}
