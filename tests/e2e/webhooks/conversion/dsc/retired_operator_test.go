package dsc //nolint:testpackage // The wire fixture helper is intentionally package-private.

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"

	. "github.com/onsi/gomega"
)

func TestRetiredOperatorDSCWireCases(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"trainingoperator", "llamastackoperator"} {
		for _, testCase := range []struct {
			name    string
			present bool
			state   operatorv1.ManagementState
			found   bool
		}{
			{name: "explicit_removed", present: true, state: operatorv1.Removed, found: true},
			{name: "empty", present: true},
			{name: "omitted"},
		} {
			t.Run(field+"/"+testCase.name, func(t *testing.T) {
				t.Parallel()
				g := NewWithT(t)
				object := retiredOperatorDSC(dscv2.GroupVersion.String(), field, testCase.present, testCase.state)
				g.Expect(object.GetAPIVersion()).To(Equal(dscv2.GroupVersion.String()))
				g.Expect(object.GetName()).To(Equal(webhookSuiteDSCName))
				component, present, err := unstructured.NestedMap(object.Object, "spec", "components", field)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(present).To(Equal(testCase.present))
				if !present {
					return
				}
				state, found, err := unstructured.NestedString(component, "managementState")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(Equal(testCase.found))
				if found {
					g.Expect(state).To(Equal(string(operatorv1.Removed)))
				}
			})
		}
	}
}
