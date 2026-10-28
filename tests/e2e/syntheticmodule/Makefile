IMG ?= synthetic-module-operator:latest
CONTAINER_TOOL ?= podman

.PHONY: build generate test fmt image

build:
	CGO_ENABLED=0 go build -trimpath -o bin/synthetic-module ./cmd

generate:
	../../../bin/controller-gen object:headerFile=../../../hack/boilerplate.go.txt paths=./...
	../../../bin/controller-gen crd paths=./api/... output:crd:artifacts:config=config/crd

test:
	go test ./...

fmt:
	gofmt -w api cmd internal

image:
	$(CONTAINER_TOOL) build -f Dockerfile -t $(IMG) ../../..
