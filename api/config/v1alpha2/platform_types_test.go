package v1alpha2

import (
	"testing"

	. "github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

func TestPlatformModulesEnabledModules(t *testing.T) {
	t.Parallel()

	modulesByHandler := []struct {
		name    string
		module  func(*PlatformModules) *common.ManagementSpec
		handler string
	}{
		{
			name: "AI Pipelines",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.AIPipelines
			},
			handler: "aipipelines",
		},
		{
			name: "AI Gateway",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.AIGateway
			},
			handler: "aigateway",
		},
		{
			name: "MLflow Operator",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.MLflowOperator
			},
			handler: "mlflowoperator",
		},
		{
			name: "Monitoring",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.Monitoring
			},
			handler: "monitoring",
		},
		{
			name: "MCP Lifecycle Operator",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.MCPLifecycleOperator
			},
			handler: "mcplifecycleoperator",
		},
		{
			name: "KServe",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.Kserve
			},
			handler: "kserve",
		},
		{
			name: "Trainer",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.Trainer
			},
			handler: "trainer",
		},
		{
			name: "Workbenches",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.Workbenches
			},
			handler: "workbenches",
		},
		{
			name: "OGX",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.OGX
			},
			handler: "ogx",
		},
		{
			name: "Data",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.Data
			},
			handler: "feastoperator",
		},
		{
			name: "Dashboard",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.Dashboard
			},
			handler: "dashboard",
		},
		{
			name: "Spark Operator",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.SparkOperator
			},
			handler: "sparkoperator",
		},
		{
			name: "Ray",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.Ray
			},
			handler: "ray",
		},
		{
			name: "TrustyAI",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.TrustyAI
			},
			handler: "trustyai",
		},
		{
			name: "AI Hub",
			module: func(modules *PlatformModules) *common.ManagementSpec {
				return &modules.AIHub
			},
			handler: "modelregistry",
		},
	}

	for _, tc := range modulesByHandler {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, state := range []struct {
				name  string
				state operatorv1.ManagementState
				want  []string
			}{
				{name: "Managed", state: operatorv1.Managed, want: []string{tc.handler}},
				{name: "Removed", state: operatorv1.Removed},
				{name: "empty"},
			} {
				t.Run(state.name, func(t *testing.T) {
					t.Parallel()

					modules := &PlatformModules{}
					tc.module(modules).ManagementState = state.state

					g := NewWithT(t)
					g.Expect(modules.EnabledModules()).To(Equal(state.want))
				})
			}
		})
	}

	t.Run("combined modules retain field order", func(t *testing.T) {
		g := NewWithT(t)
		modules := &PlatformModules{}
		want := make([]string, 0, len(modulesByHandler))
		for _, tc := range modulesByHandler {
			tc.module(modules).ManagementState = operatorv1.Managed
			want = append(want, tc.handler)
		}
		g.Expect(modules.EnabledModules()).To(Equal(want))
	})
}
