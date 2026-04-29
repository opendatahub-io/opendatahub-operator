package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
)

// registerRecentEvents adds the recent_events tool to MCP server.
func registerRecentEvents(s *server.MCPServer, kubeClient client.Client) {
	tool := mcp.NewTool("recent_events",
		mcp.WithDescription("Warning/error events in ODH namespaces. "+
			"Returns recent Kubernetes events sorted by last timestamp (most recent first). "+
			"Auto-discovers ODH namespaces from DSCI if namespace is not specified."),
		mcp.WithString("namespace",
			mcp.Description("Comma-separated namespaces to query. Omit to auto-discover from DSCI.")),
		mcp.WithString("since",
			mcp.Description("Go duration for the look-back window (e.g. 5m, 1h). Default: 5m")),
		mcp.WithString("event_type",
			mcp.Description("Filter by event type: Warning, Normal. Omit for all types.")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sinceStr := stringParam(req, "since", "5m")
		sinceDur, err := time.ParseDuration(sinceStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid since duration %q: %v", sinceStr, err)), nil
		}
		if sinceDur <= 0 {
			return mcp.NewToolResultError(fmt.Sprintf("since duration must be positive, got %q", sinceStr)), nil
		}

		eventType := strings.TrimSpace(stringParam(req, "event_type", ""))
		switch strings.ToLower(eventType) {
		case "":
		case "warning":
			eventType = "Warning"
		case "normal":
			eventType = "Normal"
		default:
			return mcp.NewToolResultError(fmt.Sprintf("invalid event_type %q: must be Warning, Normal, or omitted", eventType)), nil
		}

		cfg := clusterhealth.RecentEventsConfig{
			Client:    kubeClient,
			Since:     sinceDur,
			EventType: eventType,
		}
		if ns := stringParam(req, "namespace", ""); ns != "" {
			cfg.Namespaces = splitTrimmed(ns)
		} else {
			discovered, discErr := discoverODHNamespaces(ctx, kubeClient)
			if discErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("namespace discovery failed: %v", discErr)), nil
			}
			cfg.Namespaces = discovered
		}

		events, eventsErr := clusterhealth.RunRecentEvents(ctx, cfg)
		if eventsErr != nil && len(events) == 0 {
			return mcp.NewToolResultError(fmt.Sprintf("recent_events error: %v", eventsErr)), nil
		}

		data, err := json.MarshalIndent(events, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("json marshal error: %v", err)), nil
		}

		result := string(data)
		if eventsErr != nil {
			result = fmt.Sprintf("warning: partial failure: %v\n\n%s", eventsErr, result)
		}
		return mcp.NewToolResultText(result), nil
	})
}

// discoverODHNamespaces reads the DSCI singleton to find .spec.applicationsNamespace,
// combined with the operator namespace for a deduplicated list.
func discoverODHNamespaces(ctx context.Context, kubeClient client.Client) ([]string, error) {
	appsNS := getEnvDefault(envApplicationsNamespace, defaultAppsNS)
	operatorNS := getEnvDefault(envOperatorNamespace, defaultOperatorNS)

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   clusterhealth.DSCInitializationGVK.Group,
		Version: clusterhealth.DSCInitializationGVK.Version,
		Kind:    clusterhealth.DSCInitializationGVK.Kind + "List",
	})
	if err := kubeClient.List(ctx, list); err != nil {
		return nil, fmt.Errorf("failed to list DSCI: %w", err)
	}

	if len(list.Items) > 0 {
		if ns, found, _ := unstructured.NestedString(list.Items[0].Object, "spec", "applicationsNamespace"); found && ns != "" {
			appsNS = ns
		}
	}

	if operatorNS == appsNS {
		return []string{appsNS}, nil
	}
	return []string{appsNS, operatorNS}, nil
}
