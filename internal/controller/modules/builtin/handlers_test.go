package builtin_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	modulebuiltin "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

func TestRegistrationsHaveCompleteMetadata(t *testing.T) {
	t.Parallel()

	registrations := modulebuiltin.Registrations()
	require.NotEmpty(t, registrations)

	seen := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		require.NotNil(t, registration.Handler)

		name := registration.Handler.GetName()
		require.NotEmpty(t, name)
		_, duplicate := seen[name]
		assert.False(t, duplicate, "duplicate registration for module %q", name)
		seen[name] = struct{}{}

		assert.NotEqual(t, dag.Runlevel{}, registration.Runlevel,
			"module %q has empty runlevel", name)
		assert.NotEqual(t, dag.RL(99), registration.Runlevel,
			"module %q has default runlevel", name)
		assert.Contains(t, []modules.ConfigSource{
			modules.ConfigFromDSC,
			modules.ConfigFromDSCI,
		}, registration.ConfigSource, "module %q has invalid config source", name)

		if name == serviceApi.MonitoringServiceName {
			assert.Equal(t, modules.ConfigFromDSCI, registration.ConfigSource)
		} else {
			assert.Equal(t, modules.ConfigFromDSC, registration.ConfigSource)
		}
	}
}

func TestNamesDerivedFromRegistrations(t *testing.T) {
	t.Parallel()

	registrations := modulebuiltin.Registrations()
	names := modulebuiltin.Names()
	require.Len(t, names, len(registrations))

	for i, registration := range registrations {
		assert.Equal(t, registration.Handler.GetName(), names[i])
	}
}

func TestRegisterPreservesConfigSource(t *testing.T) {
	t.Parallel()

	reg := &modules.Registry{}
	modulebuiltin.Register(reg)

	var dsciNames []string
	err := reg.ForConfigSource(modules.ConfigFromDSCI, func(handler modules.ModuleHandler, _ bool) error {
		dsciNames = append(dsciNames, handler.GetName())
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{serviceApi.MonitoringServiceName}, dsciNames)
}
