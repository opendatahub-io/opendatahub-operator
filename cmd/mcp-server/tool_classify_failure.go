package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/failureclassifier"
)

// registerClassifyFailure adds the classify_failure tool to the MCP server.
func registerClassifyFailure(s *server.MCPServer, kubeClient client.Client) {
	tool := mcp.NewTool("classify_failure",
		mcp.WithDescription("Run cluster health checks and classify the failure "+
			"deterministically. Returns a FailureClassification with category, "+
			"subcategory, error code, evidence, and confidence."),
		mcp.WithString("sections",
			mcp.Description("Comma-separated sections: nodes,deployments,pods,"+
				"events,quotas,operator,dsci,dsc. Omit for all.")),
		mcp.WithString("layer",
			mcp.Description("Comma-separated layers: infrastructure,workload,"+
				"operator. Ignored if sections is set. Omit for all.")),
		mcp.WithString("operator_namespace",
			mcp.Description("Operator namespace. Auto-discovered from E2E_TEST_OPERATOR_NAMESPACE env var or defaults to opendatahub-operator-system.")),
		mcp.WithString("applications_namespace",
			mcp.Description("Apps namespace. Auto-discovered from DSCI if not provided. Returns an error if DSCI discovery fails due to RBAC or missing CRD. Falls back to E2E_TEST_APPLICATIONS_NAMESPACE env var or 'opendatahub'.")),
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
				return discoveryErrorResult("classify_failure", err), nil
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

		if sections := stringParam(req, "sections", ""); sections != "" {
			cfg.OnlySections = splitTrimmed(sections)
		} else if layers := stringParam(req, "layer", ""); layers != "" {
			cfg.Layers = splitTrimmed(layers)
		}

		report, err := clusterhealth.Run(ctx, cfg)
		if err != nil {
			log.Printf("classify_failure: %v", err)
			return mcp.NewToolResultError("failed to run cluster health checks"), nil
		}

		fc := failureclassifier.Classify(report)

		data, err := json.MarshalIndent(fc, "", "  ")
		if err != nil {
			log.Printf("classify_failure: json marshal: %v", err)
			return mcp.NewToolResultError("failed to format failure classification"), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})
}
