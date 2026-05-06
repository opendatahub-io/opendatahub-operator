package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

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
			mcp.Description("Operator namespace. Auto-discovered from env or defaults to opendatahub-operator-system.")),
		mcp.WithString("applications_namespace",
			mcp.Description("Apps namespace. Auto-discovered from DSCI if not provided. Returns an error if DSCI discovery fails due to RBAC or missing CRD. Falls back to env var or 'opendatahub' when DSCI is absent.")),
		mcp.WithBoolean("summary",
			mcp.Description("If true, return a compact summary instead of the full report. Default: true")),
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
				return discoveryErrorResult("platform_health", err), nil
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
			log.Printf("platform_health: %v", err)
			return mcp.NewToolResultError("failed to run cluster health checks"), nil
		}

		var output any
		if boolParam(req, "summary", true) {
			output = summarizeReport(report)
		} else {
			output = report
		}

		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			log.Printf("platform_health: json marshal: %v", err)
			return mcp.NewToolResultError("failed to format health report"), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})
}

type HealthSummary struct {
	Healthy     bool                      `json:"healthy"`
	CollectedAt time.Time                 `json:"collectedAt"`
	Sections    map[string]SectionSummary `json:"sections"`
}

type SectionSummary struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Count  int    `json:"count"`
	Issues int    `json:"issues"`
}

func section[T any](err string, items []T, isUnhealthy func(T) bool) SectionSummary {
	issues := 0
	for _, item := range items {
		if isUnhealthy(item) {
			issues++
		}
	}
	status := "ok"
	if err != "" {
		status = "error"
	}
	return SectionSummary{Status: status, Error: err, Count: len(items), Issues: issues}
}

func flattenMap[T any](m map[string][]T) []T {
	var out []T
	for _, v := range m {
		out = append(out, v...)
	}
	return out
}

func summarizeReport(report *clusterhealth.Report) *HealthSummary {
	ran := make(map[string]bool, len(report.SectionsRun))
	for _, name := range report.SectionsRun {
		ran[name] = true
	}

	all := map[string]SectionSummary{
		"nodes":       section(report.Nodes.Error, report.Nodes.Data.Nodes, func(n clusterhealth.NodeInfo) bool { return n.UnhealthyReason != "" }),
		"deployments": section(report.Deployments.Error, flattenMap(report.Deployments.Data.ByNamespace), func(d clusterhealth.DeploymentInfo) bool { return d.Ready < d.Replicas }),
		"pods":        section(report.Pods.Error, flattenMap(report.Pods.Data.ByNamespace), func(p clusterhealth.PodInfo) bool { return p.Phase != "Running" && p.Phase != "Succeeded" }),
		"events":      section(report.Events.Error, report.Events.Data.Events, func(clusterhealth.EventInfo) bool { return false }),
		"quotas":      section(report.Quotas.Error, flattenMap(report.Quotas.Data.ByNamespace), func(q clusterhealth.ResourceQuotaInfo) bool { return len(q.Exceeded) > 0 }),
		"dsci":        section(report.DSCI.Error, report.DSCI.Data.Conditions, func(clusterhealth.ConditionSummary) bool { return false }),
		"dsc":         section(report.DSC.Error, report.DSC.Data.Conditions, func(clusterhealth.ConditionSummary) bool { return false }),
	}

	opStatus := "ok"
	if report.Operator.Error != "" {
		opStatus = "error"
	}
	opIssues := 0
	if d := report.Operator.Data.Deployment; d != nil && d.Ready < d.Replicas {
		opIssues = 1
	}
	all["operator"] = SectionSummary{Status: opStatus, Error: report.Operator.Error, Count: 1, Issues: opIssues}

	sections := make(map[string]SectionSummary, len(ran))
	for name, summary := range all {
		if ran[name] {
			sections[name] = summary
		}
	}

	return &HealthSummary{
		Healthy:     report.Healthy(),
		CollectedAt: report.CollectedAt,
		Sections:    sections,
	}
}
