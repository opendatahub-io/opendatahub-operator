//nolint:testpackage // Verifies handler internals such as defaults and projected fields.
package mlflowoperator

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

const (
	mlflowTestGatewayURL = "gateway.apps.example.com"
)

func TestIsEnabled(t *testing.T) {
	handler := NewHandler()

	if handler.IsEnabled(nil) {
		t.Fatalf("expected nil modules to disable module")
	}

	pm := &configApi.PlatformModules{
		MLflowOperator: common.ManagementSpec{
			ManagementState: operatorv1.Removed,
		},
	}
	if handler.IsEnabled(pm) {
		t.Fatalf("expected Removed MLflowOperator to be disabled")
	}

	pm.MLflowOperator.ManagementState = operatorv1.Managed
	if !handler.IsEnabled(pm) {
		t.Fatalf("expected Managed MLflowOperator to enable module")
	}
}

func TestIsEnabled_PlatformMode(t *testing.T) {
	handler := NewHandler()

	pm := &configApi.PlatformModules{
		MLflowOperator: common.ManagementSpec{
			ManagementState: operatorv1.Removed,
		},
	}
	if handler.IsEnabled(pm) {
		t.Fatalf("expected Removed MLflowOperator platform mode to disable module")
	}

	pm.MLflowOperator.ManagementState = operatorv1.Managed
	if !handler.IsEnabled(pm) {
		t.Fatalf("expected Managed MLflowOperator platform mode to enable module")
	}
}

func TestGetOperatorManifests(t *testing.T) {
	handler := NewHandler()

	odhManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.OpenDataHub},
	})
	if len(odhManifests.Manifests) != 1 || odhManifests.Manifests[0].SourcePath != "overlays/odh" {
		t.Fatalf("expected ODH overlay, got %#v", odhManifests.Manifests)
	}

	rhoaiManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.SelfManagedRhoai},
	})
	if len(rhoaiManifests.Manifests) != 1 || rhoaiManifests.Manifests[0].SourcePath != "overlays/rhoai" {
		t.Fatalf("expected RHOAI overlay, got %#v", rhoaiManifests.Manifests)
	}

	managedRhoaiManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.ManagedRhoai},
	})
	if len(managedRhoaiManifests.Manifests) != 1 || managedRhoaiManifests.Manifests[0].SourcePath != "overlays/rhoai" {
		t.Fatalf("expected managed RHOAI overlay, got %#v", managedRhoaiManifests.Manifests)
	}

	xksManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.XKS},
	})
	if len(xksManifests.Manifests) != 1 || xksManifests.Manifests[0].SourcePath != "overlays/rhoai" {
		t.Fatalf("expected RHOAI overlay for XKS, got %#v", xksManifests.Manifests)
	}
}
