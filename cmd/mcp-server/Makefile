# MCP server targets (included from root Makefile).

.PHONY: mcp-server
mcp-server:
	cd cmd/mcp-server && go build -o ../../bin/mcp-server .

.PHONY: mcp-server-test
mcp-server-test:
	cd cmd/mcp-server && go test -count=1 ./...

.PHONY: mcp-server-integration
mcp-server-integration: mcp-server
	@echo "See cmd/mcp-server/INTEGRATION_TEST.md for manual integration test steps"
	@echo "Requires: KUBECONFIG set, ODH operator deployed, DSCI/DSC created"

.PHONY: diagnose
diagnose:
	cd cmd/mcp-server && go run . --one-shot

# diagnose with custom test name for failure classification.
.PHONY: diagnose-named
diagnose-named:
ifndef TEST_NAME
	$(error TEST_NAME is required. Usage: make diagnose-named TEST_NAME="my-failing-test")
endif
ifneq ($(shell echo '$(TEST_NAME)' | grep -qE '[;|&$$`\\!><]' && echo bad),)
	$(error TEST_NAME contains shell metacharacters. Only alphanumeric, slash, underscore, hyphen, dot, and space are allowed.)
endif
	cd cmd/mcp-server && go run . --one-shot --test-name="$(TEST_NAME)"
	
