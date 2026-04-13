package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

const maxLogBytes int64 = 50 * 1024 // 50KB

// registerPodLogs adds the pod_logs tool to the MCP server.
func registerPodLogs(s *server.MCPServer, clientset kubernetes.Interface) {
	tool := mcp.NewTool("pod_logs",
		mcp.WithDescription("Retrieve recent logs for a specific pod/container. "+
			"Uses the Kubernetes pod log API to fetch container logs for debugging."),
		mcp.WithString("pod_name", mcp.Required(),
			mcp.Description("Name of the pod")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace of the pod")),
		mcp.WithString("container",
			mcp.Description("Container name. Omit for the default container.")),
		mcp.WithBoolean("previous",
			mcp.Description("If true, return logs from the previous container instance. Default: false")),
		mcp.WithNumber("tail_lines",
			mcp.Description("Number of lines from the end of the log to return. Default: 100")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return fetchPodLogs(ctx, clientset, req)
	})
}

// fetchPodLogs retrieves pod logs using the client-go API.
func fetchPodLogs(ctx context.Context, clientset kubernetes.Interface, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := stringParam(req, "pod_name", "")
	namespace := stringParam(req, "namespace", "")

	if podName == "" {
		return mcp.NewToolResultError("pod_name is required"), nil
	}
	if namespace == "" {
		return mcp.NewToolResultError("namespace is required"), nil
	}

	tailLines := numberParam(req, "tail_lines", 100)
	args, _ := req.Params.Arguments.(map[string]any)
	prev, _ := args["previous"].(bool)
	limitBytes := maxLogBytes + 1

	opts := &corev1.PodLogOptions{
		Container:  stringParam(req, "container", ""),
		Previous:   prev,
		TailLines:  &tailLines,
		LimitBytes: &limitBytes,
	}

	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		switch {
		case k8serr.IsNotFound(err):
			return mcp.NewToolResultError(fmt.Sprintf("pod %q not found in namespace %q", podName, namespace)), nil
		case k8serr.IsBadRequest(err):
			msg := err.Error()
			switch {
			case strings.Contains(msg, "previous terminated container"):
				return mcp.NewToolResultError(fmt.Sprintf("no previous logs available for pod %q (namespace %q): %v", podName, namespace, err)), nil
			case strings.Contains(msg, "not found") || strings.Contains(msg, "is not valid"):
				return mcp.NewToolResultError(fmt.Sprintf("container not found in pod %q (namespace %q): %v", podName, namespace, err)), nil
			}
			fallthrough
		default:
			return mcp.NewToolResultError(fmt.Sprintf("pod logs error: %v", err)), nil
		}
	}
	defer stream.Close()

	data, err := io.ReadAll(io.LimitReader(stream, maxLogBytes+1))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("pod logs error: %v", err)), nil
	}

	truncated := int64(len(data)) > maxLogBytes
	if truncated {
		data = data[:maxLogBytes]
	}

	output := string(data)
	if truncated {
		output = "[truncated: output exceeded 50KB limit]\n" + output
	}

	return mcp.NewToolResultText(output), nil
}
