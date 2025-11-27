# E2E Test Image with precompiled tests
# ARG vars
ARG GOLANG_VERSION=1.24

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

# ENV vars
ENV E2E_TEST_OPERATOR_NAMESPACE=opendatahub-operators
ENV E2E_TEST_APPLICATIONS_NAMESPACE=opendatahub
ENV E2E_TEST_WORKBENCHES_NAMESPACE=opendatahub
ENV E2E_TEST_DSC_MONITORING_NAMESPACE=opendatahub

RUN apt-get update -y && \
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

CMD gotestsum --junitfile-project-name odh-operator-e2e --junitfile results/xunit_report.xml --format testname --raw-command \
-- test2json -p e2e ./e2e-tests --test.parallel=1 --test.v=test2json --deletion-policy=never \
--operator-namespace=$E2E_TEST_OPERATOR_NAMESPACE --applications-namespace=$E2E_TEST_APPLICATIONS_NAMESPACE \
--workbenches-namespace=$E2E_TEST_WORKBENCHES_NAMESPACE --dsc-monitoring-namespace=$E2E_TEST_DSC_MONITORING_NAMESPACE
