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

# Prometheus exposition format (for VictoriaMetrics import).
.PHONY: cluster-health-prometheus
cluster-health-prometheus:
	go run ./cmd/health-check -prometheus

# Long format: list conditions and details per section (like ls -l).
.PHONY: cluster-health-l
cluster-health-l:
	go run ./cmd/health-check -l


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
