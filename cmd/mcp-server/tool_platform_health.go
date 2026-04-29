package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
)

// registerPlatformHealth adds the platform_health tool to the MCP server.
func registerPlatformHealth(s *server.MCPServer, kubeClient client.Client) {
	tool := mcp.NewTool("platform_health",
		mcp.WithDescription("Run cluster health checks against an OpenDataHub "+
			"cluster and return the full report as JSON. Checks nodes, deployments, "+
			"pods, events, quotas, operator status, DSCI, and DSC."),
		mcp.WithString("sections",
			mcp.Description("Comma-separated sections: nodes,deployments,pods,"+
				"events,quotas,operator,dsci,dsc. Omit for all.")),
		mcp.WithString("layer",
			mcp.Description("Comma-separated layers: infrastructure,workload,"+
				"operator. Ignored if sections is set. Omit for all.")),
		mcp.WithString("operator_namespace",
			mcp.Description("Operator namespace. Default: opendatahub-operator-system")),
		mcp.WithString("applications_namespace",
			mcp.Description("Apps namespace. Default: opendatahub")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cfg := clusterhealth.Config{
			Client: kubeClient,
			Operator: clusterhealth.OperatorConfig{
				Namespace: stringParam(req, "operator_namespace", getEnvDefault(envOperatorNamespace, defaultOperatorNS)),
				Name:      getEnvDefault(envOperatorDeployment, defaultOperatorDeploy),
			},
			Namespaces: clusterhealth.NamespaceConfig{
				Apps:       stringParam(req, "applications_namespace", getEnvDefault(envApplicationsNamespace, defaultAppsNS)),
				Monitoring: getEnvDefault(envMonitoringNamespace, defaultMonitoringNS),
				Extra:      []string{"kube-system"},
			},
		}

		if s := stringParam(req, "sections", ""); s != "" {
			cfg.OnlySections = splitTrimmed(s)
		} else if l := stringParam(req, "layer", ""); l != "" {
			cfg.Layers = splitTrimmed(l)
		}

		report, err := clusterhealth.Run(ctx, cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("clusterhealth error: %v", err)), nil
		}

		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("json marshal error: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})
}
