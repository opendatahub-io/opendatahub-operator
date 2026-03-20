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
	cd cmd/health-check && go run .

.PHONY: cluster-health-json
cluster-health-json:
	cd cmd/health-check && go run . -json

# Long format: list conditions and details per section (like ls -l).
.PHONY: cluster-health-l
cluster-health-l:
	cd cmd/health-check && go run . -l


# Run by layer: infrastructure (nodes, quotas), workload (deployments, pods, events, operator, dsci, dsc), operator (operator, dsci, dsc).
.PHONY: cluster-health-infrastructure cluster-health-workload cluster-health-operator-layer
cluster-health-infrastructure:
	cd cmd/health-check && go run . -layer=infrastructure
cluster-health-workload:
	cd cmd/health-check && go run . -layer=workload
cluster-health-operator-layer:
	cd cmd/health-check && go run . -layer=operator

# Long format for each layer.
.PHONY: cluster-health-infrastructure-l cluster-health-workload-l cluster-health-operator-layer-l
cluster-health-infrastructure-l:
	cd cmd/health-check && go run . -layer=infrastructure -l
cluster-health-workload-l:
	cd cmd/health-check && go run . -layer=workload -l
cluster-health-operator-layer-l:
	cd cmd/health-check && go run . -layer=operator -l
