package matchers_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"

	. "github.com/onsi/gomega"
)

func TestMetadataTransforms(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	object := &unstructured.Unstructured{}

	g.Expect(matchers.SetAnnotation("example.com/state", "Managed")(object)).NotTo(HaveOccurred())
	g.Expect(matchers.SetLabel("example.com/component", "MaaS")(object)).NotTo(HaveOccurred())
	g.Expect(object).To(matchers.HaveAnnotation("example.com/state", "Managed"))
	g.Expect(object).To(matchers.HaveLabel("example.com/component", "MaaS"))
	g.Expect(matchers.RemoveAnnotation("example.com/state")(object)).NotTo(HaveOccurred())
	g.Expect(matchers.RemoveLabel("example.com/component")(object)).NotTo(HaveOccurred())
	g.Expect(object).NotTo(matchers.HaveAnnotation("example.com/state", "Managed"))
	g.Expect(object).NotTo(matchers.HaveLabel("example.com/component", "MaaS"))
}

func TestMetadataMatchersRejectNilObjects(t *testing.T) {
	t.Parallel()

	var typedNil *unstructured.Unstructured
	testCases := []struct {
		name    string
		matcher interface {
			Match(actual any) (bool, error)
		}
	}{
		{name: "annotation", matcher: matchers.HaveAnnotation("example.com/state", "Managed")},
		{name: "label", matcher: matchers.HaveLabel("example.com/component", "MaaS")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			for _, actual := range []any{nil, typedNil} {
				matched, err := tc.matcher.Match(actual)
				g.Expect(err).To(HaveOccurred())
				g.Expect(matched).To(BeFalse())
			}
		})
	}
}
