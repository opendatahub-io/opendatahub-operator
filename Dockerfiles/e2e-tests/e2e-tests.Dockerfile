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
COPY cmd/test-retry/ cmd/test-retry/
COPY pkg/ pkg/
COPY tests/ tests/

# build the e2e test binary + pre-compile the e2e tests
RUN CGO_ENABLED=${CGO_ENABLED} GOOS=linux GOARCH=${TARGETARCH} go test -c ./tests/e2e/ -o e2e-tests

# Build test-retry CLI for JUnit enrichment
RUN cd cmd/test-retry && CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o ../../test-retry .

################################################################################
FROM golang:$GOLANG_VERSION

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
COPY --from=builder /workspace/test-retry /go/bin/test-retry
COPY tests/e2e/scripts/run_e2e_tests.sh /e2e/run_e2e_tests.sh

RUN chmod +x ./e2e-tests /e2e/run_e2e_tests.sh /go/bin/test-retry

RUN mkdir -p results

# run main go command
ENTRYPOINT ["/e2e/run_e2e_tests.sh"]
