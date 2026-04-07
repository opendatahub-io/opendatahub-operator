package compare_test

import (
	"reflect"
	"testing"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/dsc/compare"

	. "github.com/onsi/gomega"
)

func getV1ComponentFieldNames() []string {
	v1Type := reflect.TypeFor[dscv1.Components]()
	fields := make([]string, 0, v1Type.NumField())
	for i := range v1Type.NumField() {
		fields = append(fields, v1Type.Field(i).Name)
	}
	return fields
}

func TestGetV2OnlyComponentFieldNames(t *testing.T) {
	t.Run("returns components that exist in v2 but not in v1", func(t *testing.T) {
		g := NewWithT(t)

		result := compare.GetV2OnlyComponentFieldNames()

		// Build expected v2-only components dynamically by comparing structs
		v1FieldNames := getV1ComponentFieldNames()
		v1Fields := make(map[string]bool)
		for _, name := range v1FieldNames {
			v1Fields[name] = true
		}

		v2Type := reflect.TypeFor[dscv2.Components]()
		var expectedV2Only []string
		for i := range v2Type.NumField() {
			fieldName := v2Type.Field(i).Name
			// Count components that are in v2 but not in v1
			if !v1Fields[fieldName] && fieldName != "AIPipelines" { // AIPipelines is renamed from DataSciencePipelines
				expectedV2Only = append(expectedV2Only, fieldName)
			}
		}

		// Verify the function returns the dynamically calculated v2-only components
		g.Expect(result).To(ConsistOf(expectedV2Only))
		// Also verify result is not empty (sanity check)
		g.Expect(result).NotTo(BeEmpty())
	})

	t.Run("excludes components that exist in both v1 and v2", func(t *testing.T) {
		g := NewWithT(t)

		result := compare.GetV2OnlyComponentFieldNames()

		// Verify that components present in v1 don't appear in the result
		v1FieldNames := getV1ComponentFieldNames()
		for _, fieldName := range v1FieldNames {
			g.Expect(result).NotTo(ContainElement(fieldName),
				"v1 component %s should not be in v2-only list", fieldName)
		}
	})

	t.Run("excludes renamed components", func(t *testing.T) {
		g := NewWithT(t)

		result := compare.GetV2OnlyComponentFieldNames()

		// AIPipelines in v2 was DataSciencePipelines in v1, so it's not v2-only
		g.Expect(result).NotTo(ContainElement("AIPipelines"))
	})
}
