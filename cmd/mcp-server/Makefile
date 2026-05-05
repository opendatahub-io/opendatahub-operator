# MCP server targets (included from root Makefile).

.PHONY: mcp-server
mcp-server:
	cd cmd/mcp-server && go build -o ../../bin/mcp-server .

.PHONY: mcp-server-test
mcp-server-test:
	cd cmd/mcp-server && go test ./...

# diagnose runs the MCP server in one-shot mode: health check + failure classification.
# Outputs combined JSON report to stdout, human-readable summary to stderr.
# Usage from CI:
#   make diagnose 2>diagnose-stderr.log >diagnose-report.json || true
#   make diagnose-named TEST_NAME="TestOdhOperator/Component_Tests/dashboard" \
#     2>diagnose-stderr.log >diagnose-report.json || true
.PHONY: diagnose
diagnose:
	cd cmd/mcp-server && go run . --one-shot

# diagnose with custom test name for failure classification.
.PHONY: diagnose-named
diagnose-named:
ifndef TEST_NAME
	$(error TEST_NAME is required. Usage: make diagnose-named TEST_NAME="my-failing-test")
endif
	cd cmd/mcp-server && go run . --one-shot --test-name="$(TEST_NAME)"
