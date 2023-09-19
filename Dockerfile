# Build the manager binary
ARG GOLANG_VERSION=1.18.4

################################################################################
FROM registry.access.redhat.com/ubi8/go-toolset:$GOLANG_VERSION as builder
WORKDIR /workspace
USER root
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY apis/ apis/
COPY controllers/ controllers/
COPY pkg/ pkg/
# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go

# Get all manifests from remote git repo to builder_local_false by script
COPY get_all_manifests.sh get_all_manifests.sh
RUN ./get_all_manifests.sh
RUN tar -czvf odh-manifests.tar.gz odh-manifests

################################################################################
FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/odh-manifests.tar.gz /opt/manifests/odh-manifests.tar.gz
USER 65532:65532  

ENTRYPOINT ["/manager"]
