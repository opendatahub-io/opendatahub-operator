ARG GOLANG_VERSION=1.19

################################################################################
FROM registry.access.redhat.com/ubi8/toolbox as manifests
ARG FETCH_MANIFESTS
ARG OVERWRITE_MANIFESTS
USER root
WORKDIR /opt
COPY get_all_manifests.sh get_all_manifests.sh
COPY odh-manifests/ /opt/odh-manifests/
RUN if [ "${FETCH_MANIFESTS}" = "true" ]; then \
      rm -rf /opt/odh-manifests/*; \
      ./get_all_manifests.sh ${OVERWRITE_MANIFESTS}; \
    fi

################################################################################
FROM registry.access.redhat.com/ubi8/go-toolset:$GOLANG_VERSION as builder
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
COPY infrastructure/ infrastructure/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go

################################################################################
FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --chown=1001:0 --from=manifests /opt/odh-manifests /opt/manifests
# Recursive change all files
RUN chown -R 1001:0 /opt/manifests &&\
    chmod -R a+r /opt/manifests
USER 1001

ENTRYPOINT ["/manager"]
