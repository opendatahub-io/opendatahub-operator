# Build the manager binary
ARG GOLANG_VERSION=1.21
ARG USE_LOCAL=false
ARG OVERWRITE_MANIFESTS=""
ARG TARGETOS
ARG TARGETARCH

################################################################################
FROM registry.access.redhat.com/ubi8/go-toolset:$GOLANG_VERSION as builder_local_false
ARG OVERWRITE_MANIFESTS
# Get all manifests from remote git repo to builder_local_false by script
USER root
WORKDIR /opt
COPY get_all_manifests.sh get_all_manifests.sh
RUN ./get_all_manifests.sh ${OVERWRITE_MANIFESTS}

################################################################################
FROM registry.access.redhat.com/ubi8/go-toolset:$GOLANG_VERSION as builder_local_true
# Get all manifests from local to builder_local_true
USER root
WORKDIR /opt
# copy local manifests to build
COPY odh-manifests/ /opt/odh-manifests/

################################################################################
FROM builder_local_${USE_LOCAL} as builder
USER root
WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY apis/ apis/
COPY components/ components/
COPY controllers/ controllers/
COPY main.go main.go
COPY pkg/ pkg/

# Build
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager main.go

################################################################################
FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --chown=1001:0 --from=builder /opt/odh-manifests /opt/manifests
# Recursive change all files
RUN chown -R 1001:0 /opt/manifests &&\
    chmod -R a+r /opt/manifests
USER 1001

ENTRYPOINT ["/manager"]
