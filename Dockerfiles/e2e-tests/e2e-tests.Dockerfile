# E2E Test Image with precompiled tests
ARG GOLANG_VERSION=1.24

FROM golang:$GOLANG_VERSION

RUN apt-get update -y && \
    curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
    chmod +x kubectl && \
    mv kubectl /usr/local/bin/ && \
    apt-get clean all

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

# Copy the go source needed for e2e tests
COPY api/ api/
COPY internal/ internal/
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY tests/ tests/

WORKDIR /e2e

COPY /tests/e2e/ .

RUN mkdir -p /results

CMD ["go run -C ./cmd/test-retry main.go e2e --verbose --junit-output=results/xunit_report.xml"]
