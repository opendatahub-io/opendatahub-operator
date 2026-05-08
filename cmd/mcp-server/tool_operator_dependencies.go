package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
)

// registerOperatorDependencies adds the operator_dependencies tool to the MCP server.
func registerOperatorDependencies(s *server.MCPServer, kubeClient client.Client) {
	tool := mcp.NewTool("operator_dependencies",
		mcp.WithDescription("Check status of dependent operators "+
			"(cert-manager, tempo, OTel, kueue, LWS, etc.). "+
			"Returns installed/missing/unhealthy status for each."),
		mcp.WithString("operator_namespace",
			mcp.Description("Operator namespace. Auto-discovered from env or defaults to opendatahub-operator-system.")),
		mcp.WithString("name",
			mcp.Description("Filter to a specific dependent by name (e.g. cert-manager). Omit for all.")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		operatorNS := stringParam(req, "operator_namespace", "")
		if operatorNS == "" {
			operatorNS = discoverOperatorNamespace()
		}

		report, err := clusterhealth.Run(ctx, clusterhealth.Config{
			Client: kubeClient,
			Operator: clusterhealth.OperatorConfig{
				Namespace: operatorNS,
				Name:      getEnvDefault(envOperatorDeployment, defaultOperatorDeploy),
			},
			OnlySections: []string{"operator"},
		})
		if err != nil {
			log.Printf("operator_dependencies: %v", err)
			return mcp.NewToolResultError("failed to check operator dependencies"), nil
		}
		deps := report.Operator.Data.DependentOperators
		if name := strings.ToLower(stringParam(req, "name", "")); name != "" {
			found := false
			for _, d := range deps {
				if d.Name == name {
					deps = []clusterhealth.DependentOperatorResult{d}
					found = true
					break
				}
			}
			if !found {
				return mcp.NewToolResultError(fmt.Sprintf("dependent operator %q not found", name)), nil
			}
		}

		data, err := json.MarshalIndent(deps, "", "  ")
		if err != nil {
			log.Printf("operator_dependencies: json marshal: %v", err)
			return mcp.NewToolResultError("failed to format dependency status"), nil
		}
		result := string(data)
		if report.Operator.Error != "" {
			log.Printf("operator_dependencies: partial failure: %s", report.Operator.Error)
			result = fmt.Sprintf("warning: partial failure checking dependencies\n\n%s", result)
		}
		return mcp.NewToolResultText(result), nil
	})
}
