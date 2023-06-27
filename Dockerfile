# Build the manager binary
ARG GOLANG_VERSION=1.18.9

FROM registry.access.redhat.com/ubi8/go-toolset:$GOLANG_VERSION as builder
ARG LOCAL_BUNDLE=odh-manifests

WORKDIR /workspace
USER root
# Copy the Go Modules manifests
ENV BUNDLE=$LOCAL_BUNDLE
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

# Add in the odh-manifests tarball
COPY $BUNDLE/ /opt/odh-manifests/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go


FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /opt/odh-manifests /opt/odh-manifests

RUN chown -R 1001:0 /opt/odh-manifests &&\
    chmod -R a+r /opt/odh-manifests

USER 1001

ENTRYPOINT ["/manager"]
