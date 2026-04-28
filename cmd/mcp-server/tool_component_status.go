package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
			mcp.Description("Apps namespace. Auto-discovered from DSCI if not provided. Falls back to env var or 'opendatahub'.")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		appsNS := stringParam(req, "applications_namespace", "")
		if appsNS == "" {
			var err error
			appsNS, err = discoverAppsNamespace(ctx, kubeClient)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("namespace discovery failed: %v", err)), nil
			}
		}

		result, err := clusterhealth.GetComponentStatus(ctx, kubeClient,
			stringParam(req, "component", ""),
			appsNS,
		)
		if err != nil {
			switch {
			case k8serr.IsForbidden(err):
				return mcp.NewToolResultError(fmt.Sprintf("RBAC insufficient: %v", err)), nil
			case meta.IsNoMatchError(err):
				return mcp.NewToolResultError(fmt.Sprintf(
					"CRD not installed: component %q requires a CRD that is not registered on this cluster",
					stringParam(req, "component", ""))), nil
			default:
				return mcp.NewToolResultError(err.Error()), nil
			}
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("json marshal error: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})
}
