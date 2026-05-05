package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const maxLogBytes int64 = 50 * 1024 // 50KB

type ContainerListEntry struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state"`
}

func containerState(s corev1.ContainerStatus) string {
	switch {
	case s.State.Running != nil:
		return "running"
	case s.State.Waiting != nil:
		if s.State.Waiting.Reason != "" {
			return "waiting: " + s.State.Waiting.Reason
		}
		return "waiting"
	case s.State.Terminated != nil:
		if s.State.Terminated.Reason != "" {
			return "terminated: " + s.State.Terminated.Reason
		}
		return "terminated"
	default:
		return "unknown"
	}
}

func listContainers(ctx context.Context, clientset kubernetes.Interface, podName, namespace string) (*mcp.CallToolResult, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return mcp.NewToolResultError(fmt.Sprintf("pod %q not found in namespace %q", podName, namespace)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("error getting pod: %v", err)), nil
	}

	toMap := func(statuses []corev1.ContainerStatus) map[string]corev1.ContainerStatus {
		m := make(map[string]corev1.ContainerStatus, len(statuses))
		for _, s := range statuses {
			m[s.Name] = s
		}
		return m
	}

	var entries []ContainerListEntry
	add := func(name, ctype string, statuses map[string]corev1.ContainerStatus) {
		e := ContainerListEntry{Name: name, Type: ctype}
		if s, ok := statuses[name]; ok {
			e.Ready, e.RestartCount, e.State = s.Ready, s.RestartCount, containerState(s)
		}
		entries = append(entries, e)
	}

	initS := toMap(pod.Status.InitContainerStatuses)
	for _, c := range pod.Spec.InitContainers {
		add(c.Name, "init", initS)
	}
	regS := toMap(pod.Status.ContainerStatuses)
	for _, c := range pod.Spec.Containers {
		add(c.Name, "regular", regS)
	}
	ephS := toMap(pod.Status.EphemeralContainerStatuses)
	for _, c := range pod.Spec.EphemeralContainers {
		add(c.Name, "ephemeral", ephS)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("json marshal error: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

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
		mcp.WithBoolean("list_containers",
			mcp.Description("If true, return a list of all containers instead of logs. Default: false")),
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

	if boolParam(req, "list_containers", false) {
		return listContainers(ctx, clientset, podName, namespace)
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
				errMsg := fmt.Sprintf("container not found in pod %q (namespace %q): %v", podName, namespace, err)
				if result, listErr := listContainers(ctx, clientset, podName, namespace); listErr == nil && !result.IsError {
					if len(result.Content) > 0 {
						if tc, ok := result.Content[0].(mcp.TextContent); ok {
							errMsg += "\n\nAvailable containers:\n" + tc.Text
						}
					}
				}
				return mcp.NewToolResultError(errMsg), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("invalid pod log request for pod %q (namespace %q): %v", podName, namespace, err)), nil
		case k8serr.IsForbidden(err):
			return mcp.NewToolResultError(fmt.Sprintf(
				"RBAC insufficient: the operator service-account lacks permission to read pod logs in namespace %q",
				namespace)), nil
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
