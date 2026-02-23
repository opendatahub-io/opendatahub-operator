# E2E Test Image with precompiled tests
ARG GOLANG_VERSION=1.25

################################################################################
FROM registry.access.redhat.com/ubi9/go-toolset:$GOLANG_VERSION as builder
ARG CGO_ENABLED=1
ARG TARGETARCH
USER root
WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

# Copy the go source needed for e2e tests
COPY api/ api/
COPY internal/ internal/
COPY cmd/main.go cmd/main.go
COPY pkg/ pkg/
COPY tests/ tests/

# build the e2e test binary + pre-compile the e2e tests
RUN CGO_ENABLED=${CGO_ENABLED} GOOS=linux GOARCH=${TARGETARCH} go test -c ./tests/e2e/ -o e2e-tests

################################################################################
FROM golang:$GOLANG_VERSION

# ENV vars for the go test command options
ENV E2E_TEST_OPERATOR_NAMESPACE=opendatahub-operators
ENV E2E_TEST_APPLICATIONS_NAMESPACE=opendatahub
ENV E2E_TEST_WORKBENCHES_NAMESPACE=opendatahub
ENV E2E_TEST_DSC_MONITORING_NAMESPACE=opendatahub
ENV E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES=false
ENV E2E_TEST_DELETION_POLICY=never
ENV E2E_TEST_FAIL_FAST_ON_ERROR=false
ENV E2E_TEST_OPERATOR_CONTROLLER=true
ENV E2E_TEST_OPERATOR_RESILIENCE=true
ENV E2E_TEST_OPERATOR_V2TOV3UPGRADE=true
ENV E2E_TEST_HARDWARE_PROFILE=true
ENV E2E_TEST_WEBHOOK=true
ENV E2E_TEST_COMPONENTS=true
ENV E2E_TEST_SERVICES=true

RUN apt-get update -y && apt-get upgrade -y && \
    curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
    chmod +x kubectl && \
    mv kubectl /usr/local/bin/ && \
    apt-get clean all

# install gotestsum and build test2json
RUN go install gotest.tools/gotestsum@latest \
 && go build -o /usr/local/bin/test2json cmd/test2json

WORKDIR /e2e

COPY --from=builder /workspace/e2e-tests .

RUN chmod +x ./e2e-tests

RUN mkdir -p results

# run main go command
CMD gotestsum --junitfile-project-name odh-operator-e2e \
--junitfile results/xunit_report.xml --format testname --raw-command \
-- test2json -p e2e ./e2e-tests --test.v=test2json --test.parallel=8 \
--deletion-policy="$E2E_TEST_DELETION_POLICY" --clean-up-previous-resources="$E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES" \
--test-operator-controller="$E2E_TEST_OPERATOR_CONTROLLER" --test-operator-resilience="$E2E_TEST_OPERATOR_RESILIENCE" \
--test-operator-v2tov3upgrade="$E2E_TEST_OPERATOR_V2TOV3UPGRADE" --test-hardware-profile="$E2E_TEST_HARDWARE_PROFILE" \
--test-webhook="$E2E_TEST_WEBHOOK" --test-components="$E2E_TEST_COMPONENTS" --test-services="$E2E_TEST_SERVICES" \
--operator-namespace="$E2E_TEST_OPERATOR_NAMESPACE" --applications-namespace="$E2E_TEST_APPLICATIONS_NAMESPACE" \
--workbenches-namespace="$E2E_TEST_WORKBENCHES_NAMESPACE" --dsc-monitoring-namespace="$E2E_TEST_DSC_MONITORING_NAMESPACE" \
--fail-fast-on-error="$E2E_TEST_FAIL_FAST_ON_ERROR"
