ARG GOLANG_VERSION=1.18.9

# this toolbox build only uses the local "odh-manifests" ensure get it before build image from this dockerfile
FROM registry.access.redhat.com/ubi8/go-toolset:$GOLANG_VERSION as builder
USER root
COPY odh-manifests/ /opt/odh-manifests/
WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY apis/ apis/
COPY components/ components/
COPY controllers/ controllers/
COPY main.go main.go
COPY pkg/ pkg/

RUN go install github.com/go-delve/delve/cmd/dlv@latest
RUN GOOS=linux GOARCH=amd64 go build -a -o manager main.go

################################################################################
FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
WORKDIR /
COPY --from=builder /usr/bin/dlv .
COPY --from=builder /workspace/manager .
COPY --chown=1001:0 --from=builder /opt/odh-manifests /opt/manifests
# Recursive change all files
RUN chown -R 1001:0 /opt/manifests &&\
    chmod -R a+r /opt/manifests
USER 1001

ENTRYPOINT [ "/dlv"]