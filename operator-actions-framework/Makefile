PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
LOCALBIN ?= $(PROJECT_DIR)/bin

GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.5.0

.DEFAULT_GOAL := help

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: golangci-lint ## Format code and imports.
	go fmt ./...
	$(GOLANGCI_LINT) fmt

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: tidy
tidy: ## Run go mod tidy.
	go mod tidy

##@ Testing

.PHONY: test
test: ## Run all tests.
	go test ./... -count=1

.PHONY: test-v
test-v: ## Run all tests with verbose output.
	go test ./... -count=1 -v

.PHONY: test-cover
test-cover: ## Run tests with coverage report.
	go test ./... -count=1 -coverprofile=cover.out
	go tool cover -func=cover.out
	@rm -f cover.out

##@ Build

.PHONY: build
build: ## Build all packages.
	go build ./...

##@ Linting

.PHONY: lint
lint: golangci-lint ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run --timeout=5m

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint with auto-fix.
	$(GOLANGCI_LINT) run --fix --timeout=5m

##@ Verification

.PHONY: verify
verify: tidy fmt vet lint build test ## Run all checks (tidy, fmt, vet, lint, build, test).

##@ Tools

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: clean
clean: ## Remove build artifacts.
	rm -rf $(LOCALBIN) cover.out

define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)@$(3)" ;\
GOBIN=$(LOCALBIN) go install $(2)@$(3) ;\
rm -rf $$TMP_DIR ;\
}
endef
