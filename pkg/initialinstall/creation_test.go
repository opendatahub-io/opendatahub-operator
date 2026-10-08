//nolint:testpackage // testing the unexported default-DSC builder
package initialinstall

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

// TestBuildDefaultDSC_ComponentManagementStates pins the default managementState
// for every component in the DataScienceCluster created on a fresh install.
//
// It guards the GA default-on for the MCP Lifecycle Operator (OCPMCP-382) and,
// more broadly, acts as a regression guard so any future change to a component's
// default enablement is deliberate and visible in the diff.
func TestBuildDefaultDSC_ComponentManagementStates(t *testing.T) {
	dsc := buildDefaultDSC()
	if dsc.Kind != gvk.DataScienceCluster.Kind || dsc.APIVersion != gvk.DataScienceCluster.GroupVersion().String() {
		t.Fatalf("default DSC GVK = %s/%s, want %s", dsc.APIVersion, dsc.Kind, gvk.DataScienceCluster)
	}
	c := dsc.Spec.Components

	cases := map[string]struct {
		got  operatorv1.ManagementState
		want operatorv1.ManagementState
	}{
		"Dashboard":            {c.Dashboard.Standard.ManagementState, operatorv1.Managed},
		"Workbenches":          {c.Workbenches.ManagementState, operatorv1.Managed},
		"AIPipelines":          {c.AIPipelines.ManagementState, operatorv1.Managed},
		"Kserve":               {c.Kserve.ManagementState, operatorv1.Managed},
		"Ray":                  {c.Ray.ManagementState, operatorv1.Managed},
		"Kueue":                {c.Kueue.ManagementState, operatorv1.Removed},
		"TrustyAI":             {c.TrustyAI.ManagementState, operatorv1.Managed},
		"AIHub":                {c.AIHub.ManagementState, operatorv1.Managed},
		"FeatureStore":         {c.Data.FeatureStore.ManagementState, operatorv1.Managed},
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

func TestCreateDefaultDSC(t *testing.T) {
	cli, err := fakeclient.New()
	if err != nil {
		t.Fatal(err)
	}

	if err := CreateDefaultDSC(t.Context(), cli); err != nil {
		t.Fatal(err)
	}

	created := &dscApi.DataScienceCluster{}
	if err := cli.Get(t.Context(), client.ObjectKey{Name: "default-dsc"}, created); err != nil {
		t.Fatal(err)
	}
	if created.Spec.Components.MCPLifecycleOperator.ManagementState != operatorv1.Managed {
		t.Errorf("created DSC MCP Lifecycle Operator managementState = %q, want Managed", created.Spec.Components.MCPLifecycleOperator.ManagementState)
	}
}
