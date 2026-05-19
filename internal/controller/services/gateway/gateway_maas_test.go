//go:build !integration

//nolint:testpackage
package gateway

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"

	. "github.com/onsi/gomega"
)

func TestIsMaaSEnabled(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		objects  []client.Object
		expected bool
	}{
		{
			name:     "returns false when no DSC exists",
			objects:  nil,
			expected: false,
		},
		{
			name: "returns false when Kserve is Removed",
			objects: []client.Object{
				newDSC(operatorv1.Removed, operatorv1.Managed),
			},
			expected: false,
		},
		{
			name: "returns false when ModelsAsService is Removed",
			objects: []client.Object{
				newDSC(operatorv1.Managed, operatorv1.Removed),
			},
			expected: false,
		},
		{
			name: "returns false when both are Removed",
			objects: []client.Object{
				newDSC(operatorv1.Removed, operatorv1.Removed),
			},
			expected: false,
		},
		{
			name: "returns true when both Kserve and ModelsAsService are Managed",
			objects: []client.Object{
				newDSC(operatorv1.Managed, operatorv1.Managed),
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			ctx := t.Context()

			builder := setupTestClient()
			if len(tc.objects) > 0 {
				builder = builder.WithObjects(tc.objects...)
			}
			cli := builder.Build()

			result := isMaaSEnabled(ctx, cli)
			g.Expect(result).To(Equal(tc.expected))
		})
	}
}

func TestMaaSGatewayConstants(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	g.Expect(MaaSGatewayClassName).To(Equal("openshift-default"))
	g.Expect(MaaSGatewayName).To(Equal("maas-default-gateway"))
	g.Expect(MaaSGatewaySubdomain).To(Equal("maas"))
}

func newDSC(kserveState, maasState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	return &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsc",
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: kserveState,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: maasState,
						},
					},
				},
			},
		},
	}
}
