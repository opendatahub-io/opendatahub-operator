package provision

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"

	. "github.com/onsi/gomega"
)

func TestResolveManagedComponents_DefaultsEmptyStateToRemoved(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	instance := &dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Dashboard: componentApi.DSCDashboard{},
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
	}

	states := resolveManagedComponents(instance)

	g.Expect(states).To(HaveKeyWithValue(componentApi.DashboardComponentName, operatorv1.Removed))
	g.Expect(states).To(HaveKeyWithValue(componentApi.KserveComponentName, operatorv1.Managed))
}
