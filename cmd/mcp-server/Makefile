# MCP server targets (included from root Makefile).

.PHONY: mcp-server
mcp-server:
	cd cmd/mcp-server && go build -o ../../bin/mcp-server .

.PHONY: mcp-server-test
mcp-server-test:
	cd cmd/mcp-server && go test ./...
