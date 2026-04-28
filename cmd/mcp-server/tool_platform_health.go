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
			mcp.Description("Operator namespace. Auto-discovered from env or defaults to opendatahub-operator-system.")),
		mcp.WithString("applications_namespace",
			mcp.Description("Apps namespace. Auto-discovered from DSCI if not provided. Falls back to env var or 'opendatahub'.")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		operatorNS := stringParam(req, "operator_namespace", "")
		if operatorNS == "" {
			operatorNS = discoverOperatorNamespace()
		}
		appsNS := stringParam(req, "applications_namespace", "")
		if appsNS == "" {
			var err error
			appsNS, err = discoverAppsNamespace(ctx, kubeClient)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("namespace discovery failed: %v", err)), nil
			}
		}

		cfg := clusterhealth.Config{
			Client: kubeClient,
			Operator: clusterhealth.OperatorConfig{
				Namespace: operatorNS,
				Name:      getEnvDefault(envOperatorDeployment, defaultOperatorDeploy),
			},
			Namespaces: clusterhealth.NamespaceConfig{
				Apps:       appsNS,
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
