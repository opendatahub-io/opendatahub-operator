//nolint:testpackage // testing the unexported default-DSC builder
package initialinstall

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
)

// TestBuildDefaultDSC_ComponentManagementStates pins the default managementState
// for every component in the DataScienceCluster created on a fresh install.
//
// It guards the GA default-on for the MCP Lifecycle Operator (OCPMCP-382) and,
// more broadly, acts as a regression guard so any future change to a component's
// default enablement is deliberate and visible in the diff.
func TestBuildDefaultDSC_ComponentManagementStates(t *testing.T) {
	dsc := buildDefaultDSC()
	c := dsc.Spec.Components

	cases := map[string]struct {
		got  operatorv1.ManagementState
		want operatorv1.ManagementState
	}{
		"Dashboard":            {c.Dashboard.ManagementState, operatorv1.Managed},
		"Workbenches":          {c.Workbenches.ManagementState, operatorv1.Managed},
		"AIPipelines":          {c.AIPipelines.ManagementState, operatorv1.Managed},
		"Kserve":               {c.Kserve.ManagementState, operatorv1.Managed},
		"Ray":                  {c.Ray.ManagementState, operatorv1.Managed},
		"Kueue":                {c.Kueue.ManagementState, operatorv1.Removed},
		"TrustyAI":             {c.TrustyAI.ManagementState, operatorv1.Managed},
		"ModelRegistry":        {c.ModelRegistry.ManagementState, operatorv1.Managed},
		"FeastOperator":        {c.FeastOperator.ManagementState, operatorv1.Managed},
		"LlamaStackOperator":   {c.LlamaStackOperator.ManagementState, operatorv1.Removed},
		"OGX":                  {c.OGX.ManagementState, operatorv1.Managed},
		"MLflowOperator":       {c.MLflowOperator.ManagementState, operatorv1.Managed},
		"Trainer":              {c.Trainer.ManagementState, operatorv1.Managed},
		"SparkOperator":        {c.SparkOperator.ManagementState, operatorv1.Removed},
		"AIGateway":            {c.AIGateway.ManagementState, operatorv1.Removed},
		"MCPLifecycleOperator": {c.MCPLifecycleOperator.ManagementState, operatorv1.Managed},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("default DSC %s.ManagementState = %q, want %q", name, tc.got, tc.want)
			}
		})
	}
}
