package v1alpha2

import (
	"reflect"
	"sort"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

var expectedPlatformModuleNames = []string{
	"aigateway",
	"aipipelines",
	"dashboard",
	"data",
	"databaseservice",
	"kserve",
	"mcplifecycleoperator",
	"mlflowoperator",
	"aihub",
	"monitoring",
	"ogx",
	"ray",
	"sparkoperator",
	"trainer",
	"workbenches",
	"trustyai",
}

func TestPlatformModulesModuleNames(t *testing.T) {
	t.Parallel()

	names := (&PlatformModules{}).ModuleNames()
	assert.ElementsMatch(t, expectedPlatformModuleNames, names)
}

func TestPlatformModulesModuleNamesMatchesStructFields(t *testing.T) {
	t.Parallel()

	tType := reflect.TypeOf(PlatformModules{})
	require.Equal(t, len(expectedPlatformModuleNames), tType.NumField(),
		"update expectedPlatformModuleNames when PlatformModules fields change")

	names := (&PlatformModules{}).ModuleNames()
	assert.True(t, sort.StringsAreSorted(names))
}

func TestPlatformModulesEnabledModules(t *testing.T) {
	t.Parallel()

	pm := &PlatformModules{
		AIHub:     common.ManagementSpec{ManagementState: operatorv1.Managed},
		Dashboard: common.ManagementSpec{ManagementState: operatorv1.Managed},
		Data:      common.ManagementSpec{ManagementState: operatorv1.Managed},
		Kserve:    common.ManagementSpec{ManagementState: operatorv1.Removed},
		Trainer:   common.ManagementSpec{ManagementState: operatorv1.Managed},
	}

	expected := []string{"dashboard", "data", "aihub", "trainer"}
	sort.Strings(expected)

	assert.Equal(t, expected, pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesNilReceiver(t *testing.T) {
	t.Parallel()

	var pm *PlatformModules
	assert.Nil(t, pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesEmpty(t *testing.T) {
	t.Parallel()

	pm := &PlatformModules{}
	assert.Empty(t, pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesOnlyRemovedOrEmpty(t *testing.T) {
	t.Parallel()

	pm := &PlatformModules{
		Dashboard: common.ManagementSpec{ManagementState: operatorv1.Removed},
		Kserve:    common.ManagementSpec{ManagementState: ""},
	}

	assert.Empty(t, pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesAllManaged(t *testing.T) {
	t.Parallel()

	pm := &PlatformModules{
		AIPipelines:          common.ManagementSpec{ManagementState: operatorv1.Managed},
		AIGateway:            common.ManagementSpec{ManagementState: operatorv1.Managed},
		MLflowOperator:       common.ManagementSpec{ManagementState: operatorv1.Managed},
		Monitoring:           common.ManagementSpec{ManagementState: operatorv1.Managed},
		MCPLifecycleOperator: common.ManagementSpec{ManagementState: operatorv1.Managed},
		DatabaseService:      common.ManagementSpec{ManagementState: operatorv1.Managed},
		Kserve:               common.ManagementSpec{ManagementState: operatorv1.Managed},
		Trainer:              common.ManagementSpec{ManagementState: operatorv1.Managed},
		Workbenches:          common.ManagementSpec{ManagementState: operatorv1.Managed},
		OGX:                  common.ManagementSpec{ManagementState: operatorv1.Managed},
		Data:                 common.ManagementSpec{ManagementState: operatorv1.Managed},
		Dashboard:            common.ManagementSpec{ManagementState: operatorv1.Managed},
		SparkOperator:        common.ManagementSpec{ManagementState: operatorv1.Managed},
		Ray:                  common.ManagementSpec{ManagementState: operatorv1.Managed},
		AIHub:                common.ManagementSpec{ManagementState: operatorv1.Managed},
		TrustyAI:             common.ManagementSpec{ManagementState: operatorv1.Managed},
	}

	assert.Equal(t, (&PlatformModules{}).ModuleNames(), pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesSubsetOfModuleNames(t *testing.T) {
	t.Parallel()

	pm := &PlatformModules{
		AIHub:       common.ManagementSpec{ManagementState: operatorv1.Managed},
		Dashboard:   common.ManagementSpec{ManagementState: operatorv1.Managed},
		Monitoring:  common.ManagementSpec{ManagementState: operatorv1.Removed},
		Workbenches: common.ManagementSpec{ManagementState: operatorv1.Managed},
	}

	allNames := make(map[string]struct{}, len(expectedPlatformModuleNames))
	for _, name := range (&PlatformModules{}).ModuleNames() {
		allNames[name] = struct{}{}
	}

	for _, name := range pm.EnabledModules() {
		_, ok := allNames[name]
		assert.True(t, ok, "enabled module %q is not declared on PlatformModules", name)
	}
}
