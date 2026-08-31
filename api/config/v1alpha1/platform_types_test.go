package v1alpha1_test

import (
	"reflect"
	"sort"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var expectedPlatformModuleNames = []string{
	"aigateway",
	"dashboard",
	"feastoperator",
	"kserve",
	"mcplifecycleoperator",
	"mlflowoperator",
	"modelregistry",
	"monitoring",
	"ogx",
	"sparkoperator",
	"trainer",
	"workbenches",
}

func TestPlatformModulesModuleNames(t *testing.T) {
	t.Parallel()

	names := (configv1alpha1.PlatformModules{}).ModuleNames()
	assert.Equal(t, expectedPlatformModuleNames, names)
}

func TestPlatformModulesModuleNamesMatchesStructFields(t *testing.T) {
	t.Parallel()

	tType := reflect.TypeOf(configv1alpha1.PlatformModules{})
	require.Equal(t, len(expectedPlatformModuleNames), tType.NumField(),
		"update expectedPlatformModuleNames when PlatformModules fields change")

	names := (configv1alpha1.PlatformModules{}).ModuleNames()
	assert.True(t, sort.StringsAreSorted(names))
}

func TestPlatformModulesEnabledModules(t *testing.T) {
	t.Parallel()

	pm := &configv1alpha1.PlatformModules{
		Dashboard: common.ManagementSpec{ManagementState: operatorv1.Managed},
		Kserve:    common.ManagementSpec{ManagementState: operatorv1.Removed},
		Trainer:   common.ManagementSpec{ManagementState: operatorv1.Managed},
	}

	assert.Equal(t, []string{"dashboard", "trainer"}, pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesNilReceiver(t *testing.T) {
	t.Parallel()

	var pm *configv1alpha1.PlatformModules
	assert.Nil(t, pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesEmpty(t *testing.T) {
	t.Parallel()

	pm := &configv1alpha1.PlatformModules{}
	assert.Empty(t, pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesOnlyRemovedOrEmpty(t *testing.T) {
	t.Parallel()

	pm := &configv1alpha1.PlatformModules{
		Dashboard: common.ManagementSpec{ManagementState: operatorv1.Removed},
		Kserve:    common.ManagementSpec{ManagementState: ""},
	}

	assert.Empty(t, pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesAllManaged(t *testing.T) {
	t.Parallel()

	pm := &configv1alpha1.PlatformModules{
		AIGateway:            common.ManagementSpec{ManagementState: operatorv1.Managed},
		MLflowOperator:       common.ManagementSpec{ManagementState: operatorv1.Managed},
		Monitoring:           common.ManagementSpec{ManagementState: operatorv1.Managed},
		MCPLifecycleOperator: common.ManagementSpec{ManagementState: operatorv1.Managed},
		Kserve:               common.ManagementSpec{ManagementState: operatorv1.Managed},
		Trainer:              common.ManagementSpec{ManagementState: operatorv1.Managed},
		Workbenches:          common.ManagementSpec{ManagementState: operatorv1.Managed},
		OGX:                  common.ManagementSpec{ManagementState: operatorv1.Managed},
		FeastOperator:        common.ManagementSpec{ManagementState: operatorv1.Managed},
		Dashboard:            common.ManagementSpec{ManagementState: operatorv1.Managed},
		SparkOperator:        common.ManagementSpec{ManagementState: operatorv1.Managed},
		ModelRegistry:        common.ManagementSpec{ManagementState: operatorv1.Managed},
	}

	assert.Equal(t, expectedPlatformModuleNames, pm.EnabledModules())
}

func TestPlatformModulesEnabledModulesSubsetOfModuleNames(t *testing.T) {
	t.Parallel()

	pm := &configv1alpha1.PlatformModules{
		Dashboard:   common.ManagementSpec{ManagementState: operatorv1.Managed},
		Monitoring:  common.ManagementSpec{ManagementState: operatorv1.Removed},
		Workbenches: common.ManagementSpec{ManagementState: operatorv1.Managed},
	}

	allNames := make(map[string]struct{}, len(expectedPlatformModuleNames))
	for _, name := range (configv1alpha1.PlatformModules{}).ModuleNames() {
		allNames[name] = struct{}{}
	}

	for _, name := range pm.EnabledModules() {
		_, ok := allNames[name]
		assert.True(t, ok, "enabled module %q is not declared on PlatformModules", name)
	}
}
