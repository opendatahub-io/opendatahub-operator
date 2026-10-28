ARG GOLANG_VERSION=1.26

FROM registry.access.redhat.com/ubi9/go-toolset:${GOLANG_VERSION} AS builder
USER root
WORKDIR /workspace
COPY tests/e2e/syntheticmodule/go.mod tests/e2e/syntheticmodule/go.sum ./
RUN go mod download
COPY tests/e2e/syntheticmodule/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /synthetic-module ./cmd

FROM registry.access.redhat.com/ubi9/ubi-micro:latest
COPY --from=builder /synthetic-module /synthetic-module
USER 1001
ENTRYPOINT ["/synthetic-module"]
