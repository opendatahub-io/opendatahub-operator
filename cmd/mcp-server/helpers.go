package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
)

var (
	ErrDSCIRBACInsufficient = errors.New("RBAC insufficient: cannot list DSCInitialization resources")
	ErrDSCICRDNotInstalled  = errors.New("CRD not installed: DSCInitialization CRD is not registered on this cluster")
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

// boolParam extracts a boolean param from an MCP request, returning fallback if missing.
func boolParam(req mcp.CallToolRequest, name string, fallback bool) bool {
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return fallback
	}
	if v, ok := args[name].(bool); ok {
		return v
	}
	return fallback
}

// getEnvDefault returns the env var value if set, otherwise fallback.
func getEnvDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// discoverAppsNamespace reads the DSCI singleton to find spec.applicationsNamespace.
// Falls back to env var / hardcoded default if DSCI is absent or field is empty.
// Returns an error for RBAC or CRD issues that should not be silently masked.
func discoverAppsNamespace(ctx context.Context, kubeClient client.Client) (string, error) {
	fallback := getEnvDefault(envApplicationsNamespace, defaultAppsNS)

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   clusterhealth.DSCInitializationGVK.Group,
		Version: clusterhealth.DSCInitializationGVK.Version,
		Kind:    clusterhealth.DSCInitializationGVK.Kind + "List",
	})
	if err := kubeClient.List(ctx, list); err != nil {
		switch {
		case k8serr.IsForbidden(err):
			log.Printf("DSCI discovery: RBAC forbidden: %v", err)
			return "", ErrDSCIRBACInsufficient
		case meta.IsNoMatchError(err):
			log.Printf("DSCI discovery: CRD not found: %v", err)
			return "", ErrDSCICRDNotInstalled
		default:
			log.Printf("DSCI discovery: unexpected error: %v", err)
			return "", fmt.Errorf("failed to discover DSCI namespace: %w", err)
		}
	}

	if len(list.Items) > 0 {
		ns, found, nestedErr := unstructured.NestedString(list.Items[0].Object, "spec", "applicationsNamespace")
		if nestedErr != nil {
			log.Printf("DSCI discovery: invalid spec.applicationsNamespace type: %v", nestedErr)
			return "", fmt.Errorf("failed to parse DSCI spec.applicationsNamespace: %w", nestedErr)
		}
		if found && ns != "" {
			return ns, nil
		}
	}

	return fallback, nil
}

// discoveryErrorResult maps a namespace discovery error to a safe MCP error result.
// Raw error details are logged server-side; only stable messages are returned to clients.
func discoveryErrorResult(toolName string, err error) *mcp.CallToolResult {
	switch {
	case errors.Is(err, ErrDSCIRBACInsufficient):
		return mcp.NewToolResultError("namespace discovery failed: RBAC insufficient")
	case errors.Is(err, ErrDSCICRDNotInstalled):
		return mcp.NewToolResultError("namespace discovery failed: CRD not installed")
	default:
		log.Printf("%s: namespace discovery failed: %v", toolName, err)
		return mcp.NewToolResultError("namespace discovery failed")
	}
}

// discoverOperatorNamespace returns the operator namespace.
// Operator namespace is not stored in DSCI, so this wraps env var / hardcoded default.
func discoverOperatorNamespace() string {
	return getEnvDefault(envOperatorNamespace, defaultOperatorNS)
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
