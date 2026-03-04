# Cluster health check targets (included from root Makefile).
# E2E_TEST_* namespace exports — applied when this file is included.
ifndef E2E_TEST_OPERATOR_NAMESPACE
export E2E_TEST_OPERATOR_NAMESPACE = $(OPERATOR_NAMESPACE)
endif
ifndef E2E_TEST_APPLICATIONS_NAMESPACE
export E2E_TEST_APPLICATIONS_NAMESPACE = $(APPLICATIONS_NAMESPACE)
endif
ifndef E2E_TEST_WORKBENCHES_NAMESPACE
export E2E_TEST_WORKBENCHES_NAMESPACE = $(WORKBENCHES_NAMESPACE)
endif
ifndef E2E_TEST_DSC_MONITORING_NAMESPACE
export E2E_TEST_DSC_MONITORING_NAMESPACE = $(MONITORING_NAMESPACE)
endif

# cluster-health runs cluster health checks and fails if the cluster is not healthy.
.PHONY: cluster-health
cluster-health:
	go run ./cmd/health-check

.PHONY: cluster-health-json
cluster-health-json:
	go run ./cmd/health-check -json

# Long format: list conditions and details per section (like ls -l).
.PHONY: cluster-health-l
cluster-health-l:
	go run ./cmd/health-check -l

# cluster-health-<section> runs health check for a single section (nodes, deployments, pods, events, quotas, operator, dsci, dsc).
.PHONY: cluster-health-nodes cluster-health-deployments cluster-health-pods cluster-health-events cluster-health-quotas cluster-health-operator cluster-health-dsci cluster-health-dsc
cluster-health-nodes:
	go run ./cmd/health-check -sections=nodes
cluster-health-deployments:
	go run ./cmd/health-check -sections=deployments
cluster-health-pods:
	go run ./cmd/health-check -sections=pods
cluster-health-events:
	go run ./cmd/health-check -sections=events
cluster-health-quotas:
	go run ./cmd/health-check -sections=quotas
cluster-health-operator:
	go run ./cmd/health-check -sections=operator
cluster-health-dsci:
	go run ./cmd/health-check -sections=dsci
cluster-health-dsc:
	go run ./cmd/health-check -sections=dsc

# Long format for each section (conditions and details).
.PHONY: cluster-health-nodes-l cluster-health-deployments-l cluster-health-pods-l cluster-health-events-l cluster-health-quotas-l cluster-health-operator-l cluster-health-dsci-l cluster-health-dsc-l
cluster-health-nodes-l:
	go run ./cmd/health-check -sections=nodes -l
cluster-health-deployments-l:
	go run ./cmd/health-check -sections=deployments -l
cluster-health-pods-l:
	go run ./cmd/health-check -sections=pods -l
cluster-health-events-l:
	go run ./cmd/health-check -sections=events -l
cluster-health-quotas-l:
	go run ./cmd/health-check -sections=quotas -l
cluster-health-operator-l:
	go run ./cmd/health-check -sections=operator -l
cluster-health-dsci-l:
	go run ./cmd/health-check -sections=dsci -l
cluster-health-dsc-l:
	go run ./cmd/health-check -sections=dsc -l

# Run by layer: infrastructure (nodes, quotas), workload (deployments, pods, events, operator, dsci, dsc), operator (operator, dsci, dsc).
.PHONY: cluster-health-infrastructure cluster-health-workload cluster-health-operator-layer
cluster-health-infrastructure:
	go run ./cmd/health-check -layer=infrastructure
cluster-health-workload:
	go run ./cmd/health-check -layer=workload
cluster-health-operator-layer:
	go run ./cmd/health-check -layer=operator

# Long format for each layer.
.PHONY: cluster-health-infrastructure-l cluster-health-workload-l cluster-health-operator-layer-l
cluster-health-infrastructure-l:
	go run ./cmd/health-check -layer=infrastructure -l
cluster-health-workload-l:
	go run ./cmd/health-check -layer=workload -l
cluster-health-operator-layer-l:
	go run ./cmd/health-check -layer=operator -l
