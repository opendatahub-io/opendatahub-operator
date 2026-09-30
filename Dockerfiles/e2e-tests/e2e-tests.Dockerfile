# E2E Test Image with precompiled tests
ARG GOLANG_VERSION=1.26

################################################################################
FROM registry.access.redhat.com/ubi9/go-toolset:$GOLANG_VERSION as builder
ARG CGO_ENABLED=1
ARG TARGETARCH
USER root
WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
COPY pkg/clusterhealth/go.mod pkg/clusterhealth/go.mod
COPY pkg/clusterhealth/go.sum pkg/clusterhealth/go.sum
COPY pkg/failureclassifier/go.mod pkg/failureclassifier/go.mod
COPY pkg/failureclassifier/go.sum pkg/failureclassifier/go.sum
COPY pkg/scoperules/go.mod pkg/scoperules/go.mod
COPY pkg/scoperules/go.sum pkg/scoperules/go.sum

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
FROM registry.access.redhat.com/ubi9/go-toolset:$GOLANG_VERSION

USER root

RUN dnf upgrade -y && \
    curl -fLO "https://dl.k8s.io/release/$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
    chmod +x kubectl && \
    mv kubectl /usr/local/bin/ && \
    dnf install -y jq && \
    dnf clean all

# install test reporting tools and build test2json
RUN export GOBIN=/usr/local/bin \
 && go install gotest.tools/gotestsum@v1.13.0 \
 && go install github.com/jstemmer/go-junit-report/v2@v2.1.0 \
 && go build -o /usr/local/bin/test2json cmd/test2json \
 && go clean -cache -modcache

WORKDIR /e2e

COPY --from=builder /workspace/e2e-tests .
COPY --from=builder /workspace/test-retry /usr/local/bin/test-retry
COPY tests/e2e/scripts/run_e2e_tests.sh /e2e/run_e2e_tests.sh
COPY tests/e2e/scripts/e2e-scope-rules.yaml /e2e/scripts/e2e-scope-rules.yaml

RUN chmod +x ./e2e-tests /e2e/run_e2e_tests.sh /usr/local/bin/test-retry

RUN mkdir -p results && chown 1001:0 results && chmod g=u results

ENV GOPATH=/tmp/go GOCACHE=/tmp/go-build
USER 1001

ENTRYPOINT ["/e2e/run_e2e_tests.sh"]
