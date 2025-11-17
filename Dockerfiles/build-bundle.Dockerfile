# Build the bundle, golang version shouldn't matter much here, but if in doubt, keep it up-to-date with main Dockerfile
ARG GOLANG_VERSION=1.24

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
