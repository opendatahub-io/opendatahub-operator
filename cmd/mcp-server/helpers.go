package main

import (
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	envOperatorNamespace     = "E2E_TEST_OPERATOR_NAMESPACE"
	envApplicationsNamespace = "E2E_TEST_APPLICATIONS_NAMESPACE"
	envOperatorDeployment    = "E2E_TEST_OPERATOR_DEPLOYMENT_NAME"
	envMonitoringNamespace   = "E2E_TEST_DSC_MONITORING_NAMESPACE"
	defaultOperatorNS        = "opendatahub-operator-system"
	defaultAppsNS            = "opendatahub"
	defaultOperatorDeploy    = "opendatahub-operator-controller-manager"
	defaultMonitoringNS      = "opendatahub"
)

// stringParam extracts a string param from an MCP request, returning fallback if missing or empty.
func stringParam(req mcp.CallToolRequest, name, fallback string) string {
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return fallback
	}
	if v, ok := args[name].(string); ok {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

// numberParam extracts a numeric param from an MCP request, returning fallback if missing or non-positive.
func numberParam(req mcp.CallToolRequest, name string, fallback int64) int64 {
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return fallback
	}
	var val float64
	switch n := args[name].(type) {
	case float64:
		val = n
	case float32:
		val = float64(n)
	case int:
		val = float64(n)
	case int64:
		val = float64(n)
	default:
		return fallback
	}
	if val > 0 {
		return int64(val)
	}
	return fallback
}

// getEnvDefault returns the env var value if set, otherwise fallback.
func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

// splitTrimmed splits a comma-separated string into trimmed, non-empty parts.
func splitTrimmed(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
