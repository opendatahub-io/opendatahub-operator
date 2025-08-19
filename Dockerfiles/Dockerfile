# Build the manager binary
ARG GOLANG_VERSION=1.24

ARG BUILDPLATFORM
ARG TARGETPLATFORM

################################################################################
FROM --platform=$BUILDPLATFORM registry.access.redhat.com/ubi9/toolbox as manifests
ARG USE_LOCAL=false
ARG OVERWRITE_MANIFESTS=""
USER root
WORKDIR /
COPY opt/manifests/ /opt/manifests/
COPY get_all_manifests.sh get_all_manifests.sh
RUN if [ "${USE_LOCAL}" != "true" ]; then \
        rm -rf /opt/manifests/*; \
        ./get_all_manifests.sh ${OVERWRITE_MANIFESTS}; \
    fi

# Clean up unwanted directories and files from manifests
RUN rm -rf /opt/manifests/*/e2e /opt/manifests/*/scorecard /opt/manifests/*/test /opt/manifests/*/samples /opt/manifests/*/example-* \
    && find /opt/manifests -name "README.md" -delete

# Copy monitoring config removing any possibly pre-existing symlinks
RUN rm -f /opt/manifests/monitoring
COPY config/monitoring/ /opt/manifests/monitoring
# Copy ods-configs removing any possibly pre-existing symlinks
RUN rm -f /opt/manifests/osd-configs
COPY config/osd-configs/ /opt/manifests/osd-configs
# Copy kueue-configs removing any possibly pre-existing symlinks
RUN rm -f /opt/manifests/kueue-configs
COPY config/kueue-configs/ /opt/manifests/kueue-configs

################################################################################
FROM --platform=$BUILDPLATFORM registry.access.redhat.com/ubi9/go-toolset:$GOLANG_VERSION as builder
ARG CGO_ENABLED=1
ARG TARGETARCH
USER root
WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY api/ api/
COPY internal/ internal/
COPY cmd/main.go cmd/main.go
COPY pkg/ pkg/

# Build stripe out debug info to minimize binary size
RUN CGO_ENABLED=${CGO_ENABLED} GOOS=linux GOARCH=${TARGETARCH} go build -a -ldflags="-s -w" -tags strictfipsruntime -o manager cmd/main.go

################################################################################
FROM --platform=$TARGETPLATFORM registry.access.redhat.com/ubi9/ubi-minimal:latest
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --chown=1001:0 --from=manifests /opt/manifests /opt/manifests
# Recursive change all files
RUN chown -R 1001:0 /opt/manifests &&\
    chmod -R g=u /opt/manifests
USER 1001

ENTRYPOINT ["/manager"]
