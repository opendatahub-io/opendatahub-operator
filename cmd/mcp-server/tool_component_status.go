package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
)

// registerComponentStatus adds the component_status tool to the MCP server.
func registerComponentStatus(s *server.MCPServer, kubeClient client.Client) {
	tool := mcp.NewTool("component_status",
		mcp.WithDescription("Get detailed status of a specific ODH component: "+
			"CR conditions, pod statuses, and deployment readiness."),
		mcp.WithString("component", mcp.Required(),
			mcp.Description("Component name, e.g. kserve, dashboard, workbenches")),
		mcp.WithString("applications_namespace",
			mcp.Description("Apps namespace. Default: opendatahub")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := clusterhealth.GetComponentStatus(ctx, kubeClient,
			stringParam(req, "component", ""),
			stringParam(req, "applications_namespace",
				getEnvDefault(envApplicationsNamespace, defaultAppsNS)),
		)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("json marshal error: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})
}
