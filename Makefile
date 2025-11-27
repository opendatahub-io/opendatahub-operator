# IMAGE_TAG_BASE defines the opendatahub.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# opendatahub.io/opendatahub-operator-bundle:$VERSION and opendatahub.io/opendatahub-operator-catalog:$VERSION.
ifeq ($(IMAGE_TAG_BASE), )
	IMAGE_TAG_BASE = quay.io/opendatahub/opendatahub-operator
endif

# keep the name based on IMG which already used from command line
ifeq ($(IMG_TAG), )
	IMG_TAG = latest
endif
# Update IMG to a variable, to keep it consistent across versions for OpenShift CI
ifeq ($(IMG), )
	IMG ?= $(IMAGE_TAG_BASE):$(IMG_TAG)
endif
# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# default platform type
ODH_PLATFORM_TYPE ?= OpenDataHub

ifeq ($(ODH_PLATFORM_TYPE), OpenDataHub)
	# VERSION defines the project version for the bundle.
	# Update this value when you upgrade the version of your project.
	# To re-generate a bundle for another specific version without changing the standard setup, you can:
	# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
	# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
	ifeq ($(VERSION), )
		VERSION = 3.2.0
	endif
	# Specifies the namespace where the operator pods are deployed (defaults to opendatahub-operator-system)
	OPERATOR_NAMESPACE ?= opendatahub-operator-system
	# Specifies the namespace where the component deployments are deployed (defaults to opendatahub)
	APPLICATIONS_NAMESPACE ?= opendatahub
	# Specifies the namespace where the workbenches are deployed (defaults to opendatahub)
	WORKBENCHES_NAMESPACE ?= opendatahub
	# Specifies the namespace where monitoring is deployed (defaults to opendatahub)
	MONITORING_NAMESPACE ?= opendatahub
	CHANNELS ?= fast
	ROLE_NAME=controller-manager-role
	BUNDLE_DIR ?= odh-bundle
	DOCKERFILE_FILENAME=Dockerfile
	BUNDLE_DOCKERFILE_FILENAME=bundle.Dockerfile
	OPERATOR_PACKAGE=opendatahub-operator
	CONTROLLER_GEN_TAGS=--load-build-tags=odh
	CONFIG_DIR=config
	GO_RUN_ARGS=-tags=odh
else
	# VERSION defines the project version for the bundle.
	# Update this value when you upgrade the version of your project.
	# To re-generate a bundle for another specific version without changing the standard setup, you can:
	# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
	# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
	# NOTE: see also the git branches for RHOAI in get_all_manifests.sh. This variable does NOT affect those
	ifeq ($(VERSION), )
		VERSION = 3.2.0
	endif
	# Specifies the namespace where the operator pods are deployed (defaults to redhat-ods-operator)
	OPERATOR_NAMESPACE ?= redhat-ods-operator
	# Specifies the namespace where the component deployments are deployed (defaults to redhat-ods-applications)
	APPLICATIONS_NAMESPACE ?= redhat-ods-applications
	# Specifies the namespace where the workbenches are deployed (defaults to rhods-notebooks)
	WORKBENCHES_NAMESPACE ?= rhods-notebooks
	# Specifies the namespace where monitoring is deployed (defaults to redhat-ods-monitoring)
	MONITORING_NAMESPACE ?= redhat-ods-monitoring
	CHANNELS ?= alpha,stable,fast
	DEFAULT_CHANNEL ?= stable
	ROLE_NAME=rhods-operator-role
	BUNDLE_DIR ?= rhoai-bundle
	DOCKERFILE_FILENAME=rhoai.Dockerfile
	BUNDLE_DOCKERFILE_FILENAME=rhoai-bundle.Dockerfile
	OPERATOR_PACKAGE=rhods-operator
	CONTROLLER_GEN_TAGS=--load-build-tags=rhoai
	CONFIG_DIR=config/rhoai
	GO_RUN_ARGS=-tags=rhoai
endif

IMAGE_BUILDER ?= podman
DEFAULT_MANIFESTS_PATH ?= opt/manifests
CGO_ENABLED ?= 1
USE_LOCAL = false

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif

BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

##@ Build Dependencies

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
CRD_REF_DOCS ?= $(LOCALBIN)/crd-ref-docs
GINKGO ?= $(LOCALBIN)/ginkgo
YQ ?= $(LOCALBIN)/yq

## Tool Versions
KUSTOMIZE_VERSION ?= v5.7.0
CONTROLLER_TOOLS_VERSION ?= v0.17.3
OPERATOR_SDK_VERSION ?= v1.39.2
GOLANGCI_LINT_VERSION ?= v2.5.0
YQ_VERSION ?= v4.12.2
KUBE_LINTER_VERSION ?= v0.7.6
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
CRD_REF_DOCS_VERSION = 0.2.0
# Add to tool versions section
GINKGO_VERSION ?= v2.23.4


PLATFORM ?= linux/amd64

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# E2E tests additional flags
# See README.md, default go test timeout 10m
E2E_TEST_FLAGS = -timeout 50m

# Default image-build is to not use local odh-manifests folder
# set to "true" to use local instead
# see target "image-build"
IMAGE_BUILD_FLAGS ?= --build-arg USE_LOCAL=$(USE_LOCAL)
IMAGE_BUILD_FLAGS += --build-arg CGO_ENABLED=$(CGO_ENABLED)
IMAGE_BUILD_FLAGS += --platform $(PLATFORM)

# Prometheus-Unit Tests Parameters
PROMETHEUS_RULES_DIR = ./internal/controller/components
PROMETHEUS_RULE_TEMPLATES = $(shell find $(PROMETHEUS_RULES_DIR) -name "*-prometheusrules.tmpl.yaml" 2>/dev/null)
PROMETHEUS_ALERT_TESTS = $(shell find $(PROMETHEUS_RULES_DIR) -name "*-alerting.unit-tests.yaml" 2>/dev/null)

ALERT_SEVERITY = critical

# Read any custom variables overrides from a local.mk file.  This will only be read if it exists in the
# same directory as this Makefile.  Variables can be specified in the standard format supported by
# GNU Make since `include` processes any valid Makefile
# Standard variables override would include anything you would pass at runtime that is different
# from the defaults specified in this file
OPERATOR_MAKE_ENV_FILE = local.mk
-include $(OPERATOR_MAKE_ENV_FILE)

.PHONY: default
default: manifests generate lint unit-test build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

define go-mod-version
$(shell go mod graph | grep $(1) | head -n 1 | cut -d'@' -f 2)
endef

# Using controller-gen to fetch external CRDs and put them in config/crd/external folder
# They're used in tests, as they have to be created for controller to work
# Usage: $(call fetch-external-crds,module,path[,kinds])
#   module: Go module path (e.g., github.com/openshift/api)
#   path: Path within module (e.g., config/v1)
#   kinds: Optional space-separated list of specific kinds to fetch (e.g., authentication authorization)
#          If not provided, fetches all CRDs from the path
# Example: $(call fetch-external-crds,github.com/openshift/api,config/v1,authentication oauth)
define fetch-external-crds
mkdir -p $(CONFIG_DIR)/crd/external/tmp
GOFLAGS="-mod=readonly" $(CONTROLLER_GEN) crd \
paths=$(shell go env GOPATH)/pkg/mod/$(1)@$(call go-mod-version,$(1))/$(2)/... \
output:crd:artifacts:config=$(CONFIG_DIR)/crd/external/tmp
$(if $(3),$(foreach kind,$(3),find $(CONFIG_DIR)/crd/external/tmp -type f -name '*_$(kind).yaml' -exec mv {} $(CONFIG_DIR)/crd/external/ \;;))
$(if $(3),,mv $(CONFIG_DIR)/crd/external/tmp/*.yaml $(CONFIG_DIR)/crd/external/)
rm -rf $(CONFIG_DIR)/crd/external/tmp
endef

# Add all CRD base files to kustomization.yaml, skipping kustomization.yaml itself
# and avoiding duplicates by checking if each resource is already present
define add-crd-to-kustomization
mkdir -p $(CONFIG_DIR)/crd/bases && \
cd $(CONFIG_DIR)/crd/bases && \
rm -f kustomization.yaml && \
$(KUSTOMIZE) create --autodetect && \
cd -
endef

.PHONY: manifests
manifests: controller-gen kustomize ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
ifneq ($(ODH_PLATFORM_TYPE), OpenDataHub)
	$(CONTROLLER_GEN) rbac:roleName=controller-manager-role paths="./..." output:rbac:artifacts:config=config/rbac
endif
	$(CONTROLLER_GEN) $(CONTROLLER_GEN_TAGS) rbac:roleName=$(ROLE_NAME) crd:ignoreUnexportedFields=true webhook paths="./..." output:crd:artifacts:config=$(CONFIG_DIR)/crd/bases output:rbac:artifacts:config=$(CONFIG_DIR)/rbac output:webhook:artifacts:config=$(CONFIG_DIR)/webhook
	@$(call add-crd-to-kustomization)
	@$(call fetch-external-crds,github.com/openshift/api,route/v1)
	@$(call fetch-external-crds,github.com/openshift/api,user/v1)
	@$(call fetch-external-crds,github.com/openshift/api,config/v1,authentications)
CLEANFILES += config/crd/bases config/rhoai/crd/bases config/crd/external config/rhoai/crd/external config/rbac/role.yaml config/rhoai/rbac/role.yaml config/webhook/manifests.yaml config/rhoai/webhook/manifests.yaml

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

GOLANGCI_TMP_FILE = .golangci.mktmp.yml
.PHONY: fmt
fmt: golangci-lint yq ## Formats code and imports.
	go fmt ./...
	$(GOLANGCI_LINT) fmt
CLEANFILES += $(GOLANGCI_TMP_FILE)

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

GOLANGCI_LINT_TIMEOUT ?= 5m0s
.PHONY: lint
lint: golangci-lint ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run --timeout=$(GOLANGCI_LINT_TIMEOUT)

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run --fix
	$(GOLANGCI_LINT) fmt

.PHONY: kube-lint
kube-lint: prepare ## Run kube-linter against rendered manifests.
	@TMP_FILE=$$(mktemp /tmp/kube-lint.XXXXXX.yaml) && \
	$(KUSTOMIZE) build $(CONFIG_DIR)/manifests > $$TMP_FILE && \
	go run golang.stackrox.io/kube-linter/cmd/kube-linter@$(KUBE_LINTER_VERSION) lint --config .kube-linter.yaml $$TMP_FILE && \
	rm -f $$TMP_FILE

.PHONY: get-manifests
get-manifests: ## Fetch components manifests from remote git repo
	ODH_PLATFORM_TYPE=$(ODH_PLATFORM_TYPE) VERSION=$(VERSION) ./get_all_manifests.sh
CLEANFILES += opt/manifests/*

# Default to standard sed command
SED_COMMAND = sed

# macOS requires GNU sed due to BSD sed syntax differences
ifeq ($(shell uname -s),Darwin)
    # Verify gsed is available, fail with a helpful message if not installed
    ifeq ($(shell which gsed),)
        $(error gsed not found. Install with: brew install gnu-sed)
    endif
    SED_COMMAND = gsed
endif
.PHONY: api-docs
api-docs: crd-ref-docs ## Creates API docs using https://github.com/elastic/crd-ref-docs, render managementstate with marker
	$(CRD_REF_DOCS) --source-path ./ --output-path ./docs/api-overview.md --renderer markdown --config ./crd-ref-docs.config.yaml && \
	grep -Ev '\.io/[^v][^1].*)$$' ./docs/api-overview.md > temp.md && mv ./temp.md ./docs/api-overview.md && \
	$(SED_COMMAND) -i "s|](#managementstate)|](https://pkg.go.dev/github.com/openshift/api@v0.0.0-20250812222054-88b2b21555f3/operator/v1#ManagementState)|g" ./docs/api-overview.md

.PHONY: ginkgo
ginkgo: $(GINKGO)
$(GINKGO): $(LOCALBIN)
	$(call go-install-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo,$(GINKGO_VERSION))

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

RUN_ARGS = --log-mode=devel --pprof-bind-address=127.0.0.1:6060
GO_RUN_MAIN = OPERATOR_NAMESPACE=$(OPERATOR_NAMESPACE) DEFAULT_MANIFESTS_PATH=$(DEFAULT_MANIFESTS_PATH) go run $(GO_RUN_ARGS) ./cmd/main.go $(RUN_ARGS)
.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	$(GO_RUN_MAIN)

.PHONY: run-nowebhook
run-nowebhook: GO_RUN_ARGS += -tags nowebhook

run-nowebhook: manifests generate fmt vet ## Run a controller from your host without webhook enabled
	$(GO_RUN_MAIN)


.PHONY: image-build
image-build: # unit-test ## Build image with the manager.
	$(IMAGE_BUILDER) buildx build --no-cache -f Dockerfiles/$(DOCKERFILE_FILENAME) ${IMAGE_BUILD_FLAGS} -t $(IMG) .

.PHONY: image-push
image-push: ## Push image with the manager.
	$(IMAGE_BUILDER) push $(IMG)

.PHONY: image
image: image-build image-push ## Build and push image with the manager.

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: prepare
prepare: kustomize manifests manager-kustomization

# phony target for the case of changing IMG variable
.PHONY: manager-kustomization
manager-kustomization: $(CONFIG_DIR)/manager/kustomization.yaml.in
	cd $(CONFIG_DIR)/manager \
		&& cp -f kustomization.yaml.in kustomization.yaml \
		&& $(KUSTOMIZE) edit set image REPLACE_IMAGE=$(IMG)

.PHONY: install
install: prepare ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build $(CONFIG_DIR)/crd/bases | kubectl apply -f -

.PHONY: uninstall
uninstall: prepare ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build $(CONFIG_DIR)/crd/bases | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: prepare ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build $(CONFIG_DIR)/default | kubectl apply --namespace $(OPERATOR_NAMESPACE) -f -

.PHONY: undeploy
undeploy: prepare ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build $(CONFIG_DIR)/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)
CLEANFILES += $(LOCALBIN)

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: yq
yq: $(YQ) ## Download yq locally if necessary.
$(YQ): $(LOCALBIN)
	$(call go-install-tool,$(YQ),github.com/mikefarah/yq/v4,$(YQ_VERSION))

OPERATOR_SDK_DL_URL ?= https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)
.PHONY: operator-sdk
operator-sdk: $(OPERATOR_SDK) ## Download and install operator-sdk
$(OPERATOR_SDK): $(LOCALBIN)
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	test -s $(OPERATOR_SDK) || curl -sSLo $(OPERATOR_SDK) $(OPERATOR_SDK_DL_URL)/operator-sdk_$${OS}_$${ARCH} && \
	chmod +x $(OPERATOR_SDK) ;\

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

OS=$(shell uname -s)
ARCH=$(shell uname -m)
.PHONY: crd-ref-docs
crd-ref-docs: $(CRD_REF_DOCS)
$(CRD_REF_DOCS): $(LOCALBIN)
	test -s $(CRD_REF_DOCS) || ( \
		curl -sSL https://github.com/elastic/crd-ref-docs/releases/download/v$(CRD_REF_DOCS_VERSION)/crd-ref-docs_$(CRD_REF_DOCS_VERSION)_$(OS)_$(ARCH).tar.gz | tar -xzf - -C $(LOCALBIN) crd-ref-docs \
	)

.PHONY: new-component
new-component: $(LOCALBIN)/component-codegen
	$< generate $(COMPONENT)
	$(MAKE) generate manifests api-docs bundle fmt

$(LOCALBIN)/component-codegen: | $(LOCALBIN)
	cd ./cmd/component-codegen && go mod tidy && go build -o $@

WARNINGMSG = "provided API should have an example annotation"
.PHONY: bundle
bundle: prepare operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests --package $(OPERATOR_PACKAGE) --input-dir $(CONFIG_DIR)/manifests --output-dir $(CONFIG_DIR)/manifests -q
	$(KUSTOMIZE) build $(CONFIG_DIR)/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS) --package $(OPERATOR_PACKAGE) --kustomize-dir $(CONFIG_DIR)/manifests --output-dir $(BUNDLE_DIR) 2>&1 | grep -v $(WARNINGMSG)
	$(OPERATOR_SDK) bundle validate ./$(BUNDLE_DIR) 2>&1 | grep -v $(WARNINGMSG)
	$(SED_COMMAND) -i 's#COPY #COPY --from=builder /workspace/#' bundle.Dockerfile
	cat Dockerfiles/build-bundle.Dockerfile bundle.Dockerfile > Dockerfiles/$(BUNDLE_DOCKERFILE_FILENAME)
	rm bundle.Dockerfile
	rm -f $(BUNDLE_DIR)/manifests/opendatahub-operator-webhook-service_v1_service.yaml
	rm -f $(BUNDLE_DIR)/manifests/rhods-operator-webhook-service_v1_service.yaml
CLEANFILES += rhoai-bundle odh-bundle

# The bundle image is multi-stage to preserve the ability to build without invoking make
# We use build args to ensure the variables are passed to the underlying internal make invocation
.PHONY: bundle-build
bundle-build: bundle
	$(IMAGE_BUILDER) build --no-cache -f Dockerfiles/$(BUNDLE_DOCKERFILE_FILENAME) --platform $(PLATFORM) -t $(BUNDLE_IMG) \
	--build-arg BUNDLE_IMG=$(BUNDLE_IMG) \
	--build-arg IMAGE_TAG_BASE=$(IMAGE_TAG_BASE) \
	--build-arg IMG_TAG=$(IMG_TAG) \
	--build-arg OPERATOR_VERSION=$(VERSION) \
	.

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) image-push IMG=$(BUNDLE_IMG)

.PHONY: deploy-bundle
deploy-bundle: operator-sdk bundle-build bundle-push
	$(OPERATOR_SDK) run bundle $(BUNDLE_IMG)  -n $(OPERATOR_NAMESPACE)

.PHONY: upgrade-bundle
upgrade-bundle: operator-sdk bundle-build bundle-push ## Upgrade bundle
	$(OPERATOR_SDK) run bundle-upgrade $(BUNDLE_IMG) -n $(OPERATOR_NAMESPACE)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell command -v opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.55.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

.PHONY: catalog-clean
catalog-clean: ## Clean up catalog files and Dockerfile
	rm -rf catalog

.PHONY: catalog-prepare
catalog-prepare: catalog-clean opm yq ## Prepare the catalog by adding bundles to fast channel. It requires BUNDLE_IMG exists before running the target"
	mkdir -p catalog
	cp config/catalog/fbc-basic-template.yaml catalog/fbc-basic-template.yaml
	./hack/update-catalog-template.sh catalog/fbc-basic-template.yaml $(BUNDLE_IMGS)
	$(OPM) alpha render-template basic \
		--migrate-level=bundle-object-to-csv-metadata \
		-o yaml \
		catalog/fbc-basic-template.yaml > catalog/catalog.yaml
	$(OPM) validate catalog
	rm -f catalog/fbc-basic-template.yaml

# Build a catalog image using the operator package manager tool 'opm'.
# This recipe uses 'opm alpha render-template basic' to generate a catalog from a template.
# The template defines bundle images and channel relationships in a declarative way.
.PHONY: catalog-build
catalog-build: catalog-prepare
	$(IMAGE_BUILDER) build --no-cache --load -f Dockerfiles/catalog.Dockerfile --platform $(PLATFORM) -t $(CATALOG_IMG) .

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) image-push IMG=$(CATALOG_IMG)

TOOLBOX_GOLANG_VERSION := 1.24.6

# Generate a Toolbox container for locally testing changes easily
.PHONY: toolbox
toolbox: ## Create a toolbox instance with the proper Golang and Operator SDK versions
	$(IMAGE_BUILDER) build \
		--build-arg GOLANG_VERSION=$(TOOLBOX_GOLANG_VERSION) \
		--build-arg OPERATOR_SDK_VERSION=$(OPERATOR_SDK_VERSION) \
		-f Dockerfiles/toolbox.Dockerfile -t opendatahub-toolbox .
	$(IMAGE_BUILDER) stop opendatahub-toolbox ||:
	toolbox rm opendatahub-toolbox ||:
	toolbox create opendatahub-toolbox --image localhost/opendatahub-toolbox:latest

# Run tests.
TEST_SRC ?=./internal/... ./tests/integration/... ./pkg/...

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: test
test: unit-test e2e-test

.PHONY: unit-test
unit-test: envtest ginkgo # directly use ginkgo since the framework is not compatible with go test parallel
	@if [ ! -d "$(CONFIG_DIR)/crd/bases" ]; then \
		echo "Error: $(CONFIG_DIR)/crd/bases folder does not exist. Please run 'make manifests' first."; \
		exit 1; \
	fi
	OPERATOR_NAMESPACE=$(OPERATOR_NAMESPACE) KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
    	${GINKGO} -r \
        		--procs=8 \
        		--compilers=2 \
        		--timeout=15m \
        		--poll-progress-after=30s \
        		--poll-progress-interval=5s \
        		--randomize-all \
        		--randomize-suites \
        		--fail-fast \
        		--cover \
        		--coverprofile=cover.out \
        		--succinct \
        		$(TEST_SRC)
CLEANFILES += cover.out

# Pattern rule to generate .rules.yaml from PrometheusRule templates
# This finds the corresponding *-prometheusrules.tmpl.yaml in the same directory
# and extracts the spec.groups section, replacing template variables
# Note: We filter out recording rules (rules with 'record' field) to avoid conflicts with test input series
%.rules.yaml: %.unit-tests.yaml $(YQ)
	@RULE_FILE=$$(dirname $<)/$$(basename $< -alerting.unit-tests.yaml)-prometheusrules.tmpl.yaml; \
	if [ ! -f "$$RULE_FILE" ]; then \
		echo "Error: PrometheusRule template file not found: $$RULE_FILE"; \
		exit 1; \
	fi; \
	echo "Generating $@ from $$RULE_FILE (alerts only, excluding recording rules)"; \
	sed 's/{{\.Namespace}}/redhat-ods-monitoring/g; s/{{\.ApplicationNamespace}}/redhat-ods-applications/g; s/{{`{{`}}/{{/g; s/{{`}}`}}/}}/g' "$$RULE_FILE" | \
		$(YQ) eval '.spec.groups' - | \
		$(YQ) eval 'del(.[] | .rules[] | select(.alert == null))' - | \
		$(YQ) eval '{"groups": .}' - > $@

PROMETHEUS_ALERT_RULES := $(PROMETHEUS_ALERT_TESTS:.unit-tests.yaml=.rules.yaml)

# Validate PrometheusRule syntax
.PHONY: validate-prometheus-rules
validate-prometheus-rules: $(YQ)
	@echo "Validating PrometheusRule templates syntax..."
	@for tmpl_file in $(PROMETHEUS_RULE_TEMPLATES); do \
		echo "  Checking $$tmpl_file..."; \
		sed 's/{{\.Namespace}}/redhat-ods-monitoring/g; s/{{\.ApplicationNamespace}}/redhat-ods-applications/g; s/{{`{{`}}/{{/g; s/{{`}}`}}/}}/g' "$$tmpl_file" | \
			$(YQ) eval '.spec.groups' - | \
			$(YQ) eval '{"groups": .}' - | \
			promtool check rules --lint=none /dev/stdin > /dev/null || exit 1; \
	done
	@echo "✓ All PrometheusRule templates are syntactically valid"

# Run prometheus-alert-unit-tests
# Test each component separately to avoid duplicate recording rule conflicts
.PHONY: test-alerts
test-alerts: validate-prometheus-rules $(PROMETHEUS_ALERT_RULES)
	@echo "Running Prometheus alert unit tests..."
	@for test_file in $(PROMETHEUS_ALERT_TESTS); do \
		echo "  Testing $$test_file..."; \
		promtool test rules $$test_file || exit 1; \
	done
	@echo "✓ All Prometheus alert tests passed!"

#Check for alerts without unit-tests
.PHONY: check-prometheus-alert-unit-tests
check-prometheus-alert-unit-tests: $(PROMETHEUS_ALERT_RULES)
	./tests/prometheus_unit_tests/scripts/check_alert_tests.sh $(PROMETHEUS_RULES_DIR) $(ALERT_SEVERITY)
CLEANFILES += $(PROMETHEUS_ALERT_RULES)

.PHONY: e2e-test
e2e-test:
# Specifies the namespace where the operator pods are deployed
ifndef E2E_TEST_OPERATOR_NAMESPACE
export E2E_TEST_OPERATOR_NAMESPACE = $(OPERATOR_NAMESPACE)
endif
# Specifies the namespace where the component deployments are deployed
ifndef E2E_TEST_APPLICATIONS_NAMESPACE
export E2E_TEST_APPLICATIONS_NAMESPACE = $(APPLICATIONS_NAMESPACE)
endif
# Specifies the namespace where the workbenches are deployed
ifndef E2E_TEST_WORKBENCHES_NAMESPACE
export E2E_TEST_WORKBENCHES_NAMESPACE = $(WORKBENCHES_NAMESPACE)
endif
# Specifies the namespace where monitoring is deployed
ifndef E2E_TEST_DSC_MONITORING_NAMESPACE
export E2E_TEST_DSC_MONITORING_NAMESPACE = $(MONITORING_NAMESPACE)
endif
ifdef ARTIFACT_DIR
export JUNIT_OUTPUT_PATH = ${ARTIFACT_DIR}/junit_report.xml
endif
e2e-test:
	go run -C ./cmd/test-retry main.go e2e --verbose --working-dir=$(CURDIR) $(if $(JUNIT_OUTPUT_PATH),--junit-output=$(JUNIT_OUTPUT_PATH)) -- ${E2E_TEST_FLAGS}

unit-test-cli:
	go -C ./cmd/test-retry/ test ./...

.PHONY: clean
clean: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) cache clean
	chmod -R u+w $(LOCALBIN) # envtest makes its dir RO
	rm -rf $(CLEANFILES)

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
