package mcptools

import (
	"github.com/mark3labs/mcp-go/server"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegisterAll registers all ODH diagnostic MCP tools on s.
func RegisterAll(s *server.MCPServer, kubeClient client.Client) {
	registerPlatformHealth(s, kubeClient)
	registerClassifyFailure(s, kubeClient)
	registerComponentStatus(s, kubeClient)
	registerRecentEvents(s, kubeClient)
	registerOperatorDependencies(s, kubeClient)
}
