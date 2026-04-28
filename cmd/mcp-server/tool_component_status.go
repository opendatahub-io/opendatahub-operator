package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
)

type ManagedResource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

func fetchManagedResources(ctx context.Context, c client.Client, component, appsNS string) ([]ManagedResource, error) {
	opts := []client.ListOption{
		client.InNamespace(appsNS),
		client.MatchingLabels{"app.opendatahub.io/" + component: "true"},
	}
	var out []ManagedResource
	add := func(kind, name, ns string) { out = append(out, ManagedResource{kind, name, ns}) }

	var svcs corev1.ServiceList
	if err := c.List(ctx, &svcs, opts...); err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	for _, o := range svcs.Items {
		add("Service", o.Name, o.Namespace)
	}
	var cms corev1.ConfigMapList
	if err := c.List(ctx, &cms, opts...); err != nil {
		return nil, fmt.Errorf("list configmaps: %w", err)
	}
	for _, o := range cms.Items {
		add("ConfigMap", o.Name, o.Namespace)
	}
	var sas corev1.ServiceAccountList
	if err := c.List(ctx, &sas, opts...); err != nil {
		return nil, fmt.Errorf("list serviceaccounts: %w", err)
	}
	for _, o := range sas.Items {
		add("ServiceAccount", o.Name, o.Namespace)
	}
	var secs corev1.SecretList
	if err := c.List(ctx, &secs, opts...); err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}
	for _, o := range secs.Items {
		add("Secret", o.Name, o.Namespace)
	}
	return out, nil
}

// registerComponentStatus adds the component_status tool to the MCP server.
func registerComponentStatus(s *server.MCPServer, kubeClient client.Client) {
	tool := mcp.NewTool("component_status",
		mcp.WithDescription("Get detailed status of a specific ODH component: "+
			"CR conditions, pod statuses, and deployment readiness."),
		mcp.WithString("component", mcp.Required(),
			mcp.Description("Component name, e.g. kserve, dashboard, workbenches")),
		mcp.WithString("applications_namespace",
			mcp.Description("Apps namespace. Auto-discovered from DSCI if not provided. Returns an error if DSCI discovery fails due to RBAC or missing CRD. Falls back to E2E_TEST_APPLICATIONS_NAMESPACE env var or 'opendatahub'.")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		appsNS := stringParam(req, "applications_namespace", "")
		if appsNS == "" {
			var err error
			appsNS, err = discoverAppsNamespace(ctx, kubeClient)
			if err != nil {
				return discoveryErrorResult("component_status", err), nil
			}
		}

		component := stringParam(req, "component", "")
		result, err := clusterhealth.GetComponentStatus(ctx, kubeClient, component, appsNS)
		if err != nil {
			switch {
			case k8serr.IsForbidden(err):
				return mcp.NewToolResultError(fmt.Sprintf(
					"RBAC insufficient: the operator service-account lacks permission to query component %q in namespace %q",
					component, appsNS)), nil
			case meta.IsNoMatchError(err):
				return mcp.NewToolResultError(fmt.Sprintf(
					"CRD not installed: component %q requires a CRD that is not registered on this cluster",
					component)), nil
			default:
				log.Printf("component_status: failed to determine status for %q: %v", component, err)
				return mcp.NewToolResultError(fmt.Sprintf("failed to determine component status for %q", component)), nil
			}
		}

		managed, err := fetchManagedResources(ctx, kubeClient, component, appsNS)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("managed resources error: %v", err)), nil
		}

		response := struct {
			*clusterhealth.ComponentStatusResult
			ManagedResources []ManagedResource `json:"managedResources"`
		}{result, managed}

		data, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("json marshal error: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})
}
