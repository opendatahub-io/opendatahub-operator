# Build the manager binary
ARG GOLANG_VERSION=1.18.9

FROM registry.access.redhat.com/ubi8/go-toolset:$GOLANG_VERSION as builder

ARG MANIFEST_REPO="https://github.com/red-hat-data-services/odh-manifests"
ARG MANIFEST_RELEASE="master"

ARG MANIFEST_TARBALL="${MANIFEST_REPO}/tarball/${MANIFEST_RELEASE}"

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
COPY components/ components/

# Download odh-manifests tarball
ADD ${MANIFEST_TARBALL} ${MANIFEST_RELEASE}.tar.gz
RUN mkdir /opt/odh-manifests/ && \
    tar --exclude={.*,*.md,LICENSE,Makefile,Dockerfile,version.py,OWNERS,tests,ai-library} --strip-components=1 -xf ${MANIFEST_RELEASE}.tar.gz -C /opt/odh-manifests/ && \
    rm -rf ${MANIFEST_RELEASE}.tar.gz
# uncomment to test local changes
# COPY odh-manifests/ /opt/odh-manifests/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --chown=1001:0 --from=builder /opt/odh-manifests /opt/manifests
# Recursive change all files
RUN chown -R 1001:0 /opt/manifests &&\
    chmod -R a+r /opt/manifests
USER 1001

ENTRYPOINT ["/manager"]