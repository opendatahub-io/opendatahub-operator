//nolint:testpackage // white-box test for unexported gpuOperatorClusterPolicyServed
package status

import (
	"testing"

	"github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func TestGPUOperatorClusterPolicyServed(t *testing.T) {
	v1 := gvk.GPUOperatorClusterPolicy.Version

	for _, tc := range []struct {
		name     string
		versions []apiextensionsv1.CustomResourceDefinitionVersion
		want     bool
	}{
		{
			name: "empty versions list",
		},
		{
			name: "v1 served after another version",
			versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{Name: "v1alpha1", Served: true, Storage: true},
				{Name: v1, Served: true},
			},
			want: true,
		},
		{
			name: "v1 present but not served",
			versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{Name: "v1alpha1", Served: true, Storage: true},
				{Name: v1},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := gomega.NewWithT(t)

			crd := &apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{Versions: tc.versions},
			}
			g.Expect(gpuOperatorClusterPolicyServed(crd)).To(gomega.Equal(tc.want))
		})
	}
}
