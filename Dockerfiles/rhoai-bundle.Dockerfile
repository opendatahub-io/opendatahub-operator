# Build the bundle, golang version shouldn't matter much here, but if in doubt, keep it up-to-date with main Dockerfile
ARG GOLANG_VERSION=1.25

FROM registry.access.redhat.com/ubi9/go-toolset:$GOLANG_VERSION as builder
ARG IMAGE_TAG_BASE
ARG IMG_TAG
# NOTE: OPERATOR_VERSION is used to not clash with the VERSION var from go-toolset image
ARG OPERATOR_VERSION
ARG BUNDLE_IMG
USER root
WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Makefile
COPY Makefile Makefile

# Cache tools (operator-sdk, kustomize, etc)
RUN make operator-sdk controller-gen kustomize

# Copy the go source
COPY api/ api/
COPY internal/ internal/
COPY cmd/main.go cmd/main.go
COPY pkg/ pkg/

# Copy other source artifacts
# tests is needed because we have references to tests packages outside of tests
COPY tests/ tests/
COPY PROJECT PROJECT
COPY config/ config/
COPY Dockerfiles/ Dockerfiles/

RUN VERSION=$OPERATOR_VERSION make bundle
RUN VERSION=$OPERATOR_VERSION make bundle ODH_PLATFORM_TYPE=rhoai
FROM scratch

# Core bundle labels.
LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=rhods-operator
LABEL operators.operatorframework.io.bundle.channels.v1=alpha,stable,fast
LABEL operators.operatorframework.io.bundle.channel.default.v1=stable
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.39.2
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v4

# Labels for testing.
LABEL operators.operatorframework.io.test.mediatype.v1=scorecard+v1
LABEL operators.operatorframework.io.test.config.v1=tests/scorecard/

# Copy files to locations specified by labels.
COPY --from=builder /workspace/rhoai-bundle/manifests /manifests/
COPY --from=builder /workspace/rhoai-bundle/metadata /metadata/
COPY --from=builder /workspace/rhoai-bundle/tests/scorecard /tests/scorecard/
