// This file contains tests that require access to internal dashboard functions.
// These tests verify internal implementation details that are not exposed through the public API.
// These tests need to access unexported functions like CustomizeResources.
package dashboard

import (
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestCustomizeResourcesInternal(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboard := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboard,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
	}

	err = CustomizeResources(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestCustomizeResourcesNoOdhDashboardConfig(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create dashboard without ODH dashboard config
	dashboard := &componentApi.Dashboard{
		Spec: componentApi.DashboardSpec{
			// No ODH dashboard config specified
		},
	}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboard,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
	}

	// Test that CustomizeResources handles missing ODH dashboard config gracefully
	err = CustomizeResources(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}
