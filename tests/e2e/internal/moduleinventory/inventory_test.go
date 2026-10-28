package moduleinventory_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/e2e/internal/moduleinventory"
)

func TestBuiltinsMatchesProductionRegistrations(t *testing.T) {
	t.Parallel()

	inventory, err := moduleinventory.Builtins()
	require.NoError(t, err)

	registrations := builtin.Registrations()
	require.Len(t, inventory.All(), len(registrations))

	expectedNames := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		expectedNames = append(expectedNames, registration.Handler.GetName())
	}
	assert.Equal(t, expectedNames, inventory.Names())
}

func TestInventorySelect(t *testing.T) {
	t.Parallel()

	inventory, err := moduleinventory.Builtins()
	require.NoError(t, err)
	names := inventory.Names()
	require.GreaterOrEqual(t, len(names), 3)

	selected, err := inventory.Select(names[2], names[0], names[2])
	require.NoError(t, err)
	require.Len(t, selected, 2)
	assert.Equal(t, names[0], selected[0].Name)
	assert.Equal(t, names[2], selected[1].Name)
}

func TestInventorySelectEmptyReturnsAll(t *testing.T) {
	t.Parallel()

	inventory, err := moduleinventory.Builtins()
	require.NoError(t, err)

	selected, err := inventory.Select()
	require.NoError(t, err)
	assert.Equal(t, inventory.All(), selected)
}

func TestInventorySelectRejectsUnknownModules(t *testing.T) {
	t.Parallel()

	inventory, err := moduleinventory.Builtins()
	require.NoError(t, err)

	selected, err := inventory.Select("not-a-module", "also-not-a-module")
	require.Error(t, err)
	assert.Nil(t, selected)
	assert.Contains(t, err.Error(), "also-not-a-module, not-a-module")
	assert.Contains(t, err.Error(), inventory.Names()[0])
}

func TestInventoryLookup(t *testing.T) {
	t.Parallel()

	inventory, err := moduleinventory.Builtins()
	require.NoError(t, err)
	name := inventory.Names()[0]

	module, found := inventory.Lookup(name)
	require.True(t, found)
	assert.Equal(t, name, module.Name)

	_, found = inventory.Lookup("not-a-module")
	assert.False(t, found)
}

func TestInventoryForConfigSource(t *testing.T) {
	t.Parallel()

	inventory, err := moduleinventory.Builtins()
	require.NoError(t, err)

	dsciModules := inventory.ForConfigSource(modules.ConfigFromDSCI)
	require.Len(t, dsciModules, 1)
	assert.Equal(t, serviceApi.MonitoringServiceName, dsciModules[0].Name)

	dscModules := inventory.ForConfigSource(modules.ConfigFromDSC)
	assert.Len(t, dscModules, len(inventory.All())-1)
	assert.False(t, slices.ContainsFunc(dscModules, func(module moduleinventory.Module) bool {
		return module.Name == serviceApi.MonitoringServiceName
	}))
}

func TestInventoryReturnsDefensiveSlices(t *testing.T) {
	t.Parallel()

	inventory, err := moduleinventory.Builtins()
	require.NoError(t, err)

	all := inventory.All()
	names := inventory.Names()
	originalName := names[0]
	all[0].Name = "changed"
	names[0] = "changed"

	assert.Equal(t, originalName, inventory.All()[0].Name)
	assert.Equal(t, originalName, inventory.Names()[0])
}

func TestNewRejectsMissingHandler(t *testing.T) {
	t.Parallel()

	inventory, err := moduleinventory.New([]builtin.ModuleRegistration{{}})
	require.Error(t, err)
	assert.Empty(t, inventory.All())
	assert.Contains(t, err.Error(), "has no handler")
}
