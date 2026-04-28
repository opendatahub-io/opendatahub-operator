package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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
			log.Printf("recent_events: invalid since duration %q: %v", sinceStr, err)
			return mcp.NewToolResultError(fmt.Sprintf("invalid since duration %q: expected Go duration (e.g. 5m, 1h)", sinceStr)), nil
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
				return discoveryErrorResult("recent_events", discErr), nil
			}
			cfg.Namespaces = discovered
		}

		events, eventsErr := clusterhealth.RunRecentEvents(ctx, cfg)
		if eventsErr != nil && len(events) == 0 {
			log.Printf("recent_events: %v", eventsErr)
			return mcp.NewToolResultError("failed to retrieve recent events"), nil
		}

		data, err := json.MarshalIndent(events, "", "  ")
		if err != nil {
			log.Printf("recent_events: json marshal: %v", err)
			return mcp.NewToolResultError("failed to format recent events"), nil
		}

		result := string(data)
		if eventsErr != nil {
			log.Printf("recent_events: partial failure: %v", eventsErr)
			result = fmt.Sprintf("warning: partial failure retrieving events\n\n%s", result)
		}
		return mcp.NewToolResultText(result), nil
	})
}

// discoverODHNamespaces reads the DSCI singleton to find .spec.applicationsNamespace,
// combined with the operator namespace for a deduplicated list.
func discoverODHNamespaces(ctx context.Context, kubeClient client.Client) ([]string, error) {
	appsNS, err := discoverAppsNamespace(ctx, kubeClient)
	if err != nil {
		return nil, err
	}
	operatorNS := discoverOperatorNamespace()
	if operatorNS == appsNS {
		return []string{appsNS}, nil
	}
	return []string{appsNS, operatorNS}, nil
}
