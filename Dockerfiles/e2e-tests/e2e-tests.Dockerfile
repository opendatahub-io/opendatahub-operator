# E2E Test Image with precompiled tests
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

# install gotestsum and build test2json
RUN go install gotest.tools/gotestsum@latest \
 && go build -o /opt/app-root/src/test2json cmd/test2json

################################################################################
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

RUN microdnf update -y && \
    curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
    chmod +x kubectl && \
    mv kubectl /usr/local/bin/ && \
    microdnf clean all

WORKDIR /e2e

COPY --from=builder /workspace/e2e-tests .
COPY --from=builder /opt/app-root/src/go/bin/gotestsum /usr/local/bin/
COPY --from=builder /opt/app-root/src/test2json /usr/local/bin/

RUN chmod +x ./e2e-tests

RUN mkdir -p /results

ENTRYPOINT ["gotestsum", "--junitfile", "/results/xunit_report.xml", "--format", "standard-verbose", "--raw-command", "--"]
CMD ["sh", "-c", "./e2e-tests --deletion-policy=never -test.v 2>&1 | /usr/local/bin/test2json"]
