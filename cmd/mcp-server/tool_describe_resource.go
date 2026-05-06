package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const maxResponseBytes = 32 * 1024

// registerDescribeResource adds the describe_resource tool to the MCP server.
func registerDescribeResource(s *server.MCPServer, kubeClient client.Client) {
	tool := mcp.NewTool("describe_resource",
		mcp.WithDescription("Get any Kubernetes resource by apiVersion/kind/name "+
			"and optional namespace. Returns the full resource as JSON."),
		mcp.WithString("apiVersion", mcp.Required(),
			mcp.Description("API version, e.g. v1, apps/v1, datasciencecluster.opendatahub.io/v2")),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource kind, e.g. Pod, Deployment, DSCInitialization")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Resource name")),
		mcp.WithString("namespace",
			mcp.Description("Namespace. Omit for cluster-scoped resources.")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		apiVersion := stringParam(req, "apiVersion", "")
		kind := stringParam(req, "kind", "")
		name := stringParam(req, "name", "")
		namespace := stringParam(req, "namespace", "")

		if kubeClient == nil {
			return mcp.NewToolResultError("kubernetes client is not configured"), nil
		}

		if apiVersion == "" || kind == "" || name == "" {
			return mcp.NewToolResultError("apiVersion, kind, and name are required"), nil
		}

		var group, version string
		if i := strings.LastIndex(apiVersion, "/"); i >= 0 {
			group, version = apiVersion[:i], apiVersion[i+1:]
		} else {
			group, version = "", apiVersion
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   group,
			Version: version,
			Kind:    kind,
		})

		err := kubeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)
		if err != nil {
			switch {
			case k8serr.IsNotFound(err):
				if namespace != "" {
					return mcp.NewToolResultError(fmt.Sprintf("%s %q not found in namespace %q", kind, name, namespace)), nil
				}
				return mcp.NewToolResultError(fmt.Sprintf("%s %q not found", kind, name)), nil
			case k8serr.IsForbidden(err):
				return mcp.NewToolResultError(fmt.Sprintf(
					"RBAC insufficient: the operator service-account lacks permission to get %s %q", kind, name)), nil
			case meta.IsNoMatchError(err):
				return mcp.NewToolResultError(fmt.Sprintf("CRD not installed: %s in %s is not recognized", kind, apiVersion)), nil
			default:
				log.Printf("describe_resource: failed to get %s %q: %v", kind, name, err)
				return mcp.NewToolResultError(fmt.Sprintf("failed to get %s %q", kind, name)), nil
			}
		}

		// Redact sensitive fields to prevent exfiltration via MCP.
		switch {
		case strings.EqualFold(kind, "Secret"):
			unstructured.RemoveNestedField(obj.Object, "data")
			unstructured.RemoveNestedField(obj.Object, "stringData")
			_ = unstructured.SetNestedField(obj.Object, "data and stringData fields redacted for security",
				"metadata", "annotations", "mcp-server/notice")
		case strings.EqualFold(kind, "ConfigMap"):
			redactConfigMapData(obj, namespace)
		case strings.EqualFold(kind, "ServiceAccount"):
			unstructured.RemoveNestedField(obj.Object, "secrets")
			unstructured.RemoveNestedField(obj.Object, "imagePullSecrets")
			_ = unstructured.SetNestedField(obj.Object, "secrets and imagePullSecrets fields redacted for security",
				"metadata", "annotations", "mcp-server/notice")
		}

		unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
		unstructured.RemoveNestedField(obj.Object, "metadata", "annotations",
			"kubectl.kubernetes.io/last-applied-configuration")

		data, err := json.MarshalIndent(obj.Object, "", "  ")
		if err != nil {
			log.Printf("describe_resource: json marshal: %v", err)
			return mcp.NewToolResultError("failed to format resource"), nil
		}
		output := string(data)
		if len(output) > maxResponseBytes {
			output = output[:maxResponseBytes] + "\n\n... (truncated, response exceeded 32KB)"
		}
		return mcp.NewToolResultText(output), nil
	})
}

var sensitiveKeyPatterns = []string{
	"password", "passwd", "secret", "token", "apikey", "api-key",
	"credentials", "private-key", "private_key", "connection-string",
	"auth", "certificate", "tls.crt", "tls.key",
}

func redactConfigMapData(obj *unstructured.Unstructured, ns string) {
	if ns == "cert-manager-operator" ||
		strings.HasPrefix(ns, "openshift-") || strings.HasPrefix(ns, "kube-") {
		unstructured.RemoveNestedField(obj.Object, "data")
		unstructured.RemoveNestedField(obj.Object, "binaryData")
		_ = unstructured.SetNestedField(obj.Object, "all data redacted — security-sensitive namespace",
			"metadata", "annotations", "mcp-server/notice")
		return
	}

	redacted := false
	for _, field := range []string{"data", "binaryData"} {
		m, ok, _ := unstructured.NestedMap(obj.Object, field)
		if !ok {
			continue
		}
		changed := false
		for k := range m {
			lower := strings.ToLower(k)
			for _, p := range sensitiveKeyPatterns {
				if strings.Contains(lower, p) {
					m[k] = "[REDACTED]"
					redacted = true
					changed = true
					break
				}
			}
		}
		if changed {
			_ = unstructured.SetNestedField(obj.Object, m, field)
		}
	}
	if redacted {
		_ = unstructured.SetNestedField(obj.Object, "some values redacted — keys matched sensitive patterns",
			"metadata", "annotations", "mcp-server/notice")
	}
}
