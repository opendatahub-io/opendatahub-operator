package compare

import (
	"reflect"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
)

// GetV2OnlyComponentFieldNames returns the struct field names of components that exist in v2 but not v1.
// This uses reflection to automatically detect v2-only components without hardcoding,
// making it future-proof when new v2-only components are added.
//
// Returns field names like ["Trainer", "MLflowOperator", "SparkOperator"].
func GetV2OnlyComponentFieldNames() []string {
	v2OnlyFields := []string{}

	// Map of v2 field names that were renamed from v1
	// Key: v2 field name, Value: v1 field name
	renamedFields := map[string]string{
		"AIPipelines": "DataSciencePipelines", // v2's AIPipelines = v1's DataSciencePipelines
	}

	// Build a map of v1 component field names for comparison
	v1ComponentsType := reflect.TypeFor[dscv1.Components]()
	v1FieldNames := make(map[string]bool)
	for i := range v1ComponentsType.NumField() {
		v1FieldNames[v1ComponentsType.Field(i).Name] = true
	}

	// Examine v2 components to find v2-only ones
	v2ComponentsType := reflect.TypeFor[dscv2.Components]()

	for i := range v2ComponentsType.NumField() {
		field := v2ComponentsType.Field(i)
		fieldName := field.Name

		// Skip if this component exists in v1 (not v2-only)
		if v1FieldNames[fieldName] {
			continue
		}

		// Check if this v2 field has a different name in v1 (renamed component)
		if v1EquivalentName, isRenamed := renamedFields[fieldName]; isRenamed {
			if v1FieldNames[v1EquivalentName] {
				continue // Component exists in v1 under a different name
			}
		}

		// This is a v2-only component
		v2OnlyFields = append(v2OnlyFields, fieldName)
	}

	return v2OnlyFields
}
