//nolint:testpackage // testing the unexported default-DSC builder
package initialinstall

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
)

// TestBuildDefaultDSC_MCPLifecycleOperatorManaged verifies that the default
// DataScienceCluster created on a fresh install enables the MCP Lifecycle
// Operator by default (GA behavior, OCPMCP-382).
func TestBuildDefaultDSC_MCPLifecycleOperatorManaged(t *testing.T) {
	dsc := buildDefaultDSC()

	got := dsc.Spec.Components.MCPLifecycleOperator.ManagementState
	if got != operatorv1.Managed {
		t.Errorf("default DSC MCPLifecycleOperator.ManagementState = %q, want %q", got, operatorv1.Managed)
	}
}
