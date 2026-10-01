package v2_test

import (
	"reflect"
	"testing"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var expectedDSCComponentNames = []string{
	"aigateway",
	"aipipelines",
	"dashboard",
	"feastoperator",
	"kserve",
	"kueue",
	"llamastackoperator",
	"mcplifecycleoperator",
	"mlflowoperator",
	"modelregistry",
	"ogx",
	"ray",
	"sparkoperator",
	"trainer",
	"trainingoperator",
	"trustyai",
	"workbenches",
}

func TestComponentsComponentNames(t *testing.T) {
	t.Parallel()

	names := (dscv2.Components{}).ComponentNames()
	assert.Equal(t, expectedDSCComponentNames, names)
}

func TestComponentsComponentNamesMatchesStructFields(t *testing.T) {
	t.Parallel()

	tType := reflect.TypeOf(dscv2.Components{})
	require.Equal(t, len(expectedDSCComponentNames), tType.NumField(),
		"update expectedDSCComponentNames when Components fields change")

	names := (dscv2.Components{}).ComponentNames()
	assert.True(t, len(names) > 0)
	for _, name := range names {
		assert.NotEmpty(t, name)
	}
}
