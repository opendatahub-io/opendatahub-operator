# MCP server targets (included from root Makefile).

.PHONY: mcp-server
mcp-server:
	cd cmd/mcp-server && go build -o ../../bin/mcp-server .

.PHONY: mcp-server-test
mcp-server-test:
	cd cmd/mcp-server && go test ./...

.PHONY: mcp-server-integration
mcp-server-integration: mcp-server
	@echo "See cmd/mcp-server/INTEGRATION_TEST.md for manual integration test steps"
	@echo "Requires: KUBECONFIG set, ODH operator deployed, DSCI/DSC created"
