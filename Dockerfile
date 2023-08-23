# Build the manager binary
ARG GOLANG_VERSION=1.18.4
ARG LOCAL_BUNDLE=odh-manifests.tar.gz

FROM registry.access.redhat.com/ubi8/go-toolset:$GOLANG_VERSION as builder
ARG ODH_MANIFESTS_REF=v1.9.0
ARG ODH_MANIFESTS_URL=https://github.com/opendatahub-io/odh-manifests/tarball/$ODH_MANIFESTS_REF
ARG LOCAL_BUNDLE

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

# Add the local bundle and add a marker file so we know the ref this image was built from
ADD $ODH_MANIFESTS_URL $LOCAL_BUNDLE
RUN echo "$ODH_MANIFESTS_REF" > MANIFEST_VERSION && chmod g+r $LOCAL_BUNDLE
# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go


FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ARG LOCAL_BUNDLE
WORKDIR /
COPY --from=builder /workspace/manager .
COPY tests/data/test-data.tar.gz /opt/test-data/
COPY --from=builder /workspace/MANIFEST_VERSION /workspace/$LOCAL_BUNDLE /opt/manifests/
USER 65532:65532  

ENTRYPOINT ["/manager"]
