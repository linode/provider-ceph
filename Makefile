# ====================================================================================
# Setup Project
PROJECT_NAME := provider-ceph
PROJECT_REPO := github.com/linode/$(PROJECT_NAME)

PLATFORMS ?= linux_amd64 linux_arm64
-include build/makelib/common.mk

# Generate kuttl e2e tests for the following kind node versions
# TEST_KIND_NODES is not intended to be updated manually.
# Please edit LATEST_KIND_NODE instead and run 'make update-kind-nodes'.
TEST_KIND_NODES ?= 1.25.0,1.26.0,1.27.0

LATEST_KIND_NODE ?= 1.27.0
REPO ?= provider-ceph

# ====================================================================================
# Setup Output

-include build/makelib/output.mk

# ====================================================================================
# Setup Go

GO_STATIC_PACKAGES = $(GO_PROJECT)/cmd/provider
GO_LDFLAGS += -X $(GO_PROJECT)/internal/version.Version=$(VERSION)
GO_SUBDIRS += cmd internal apis
GO111MODULE = on
-include build/makelib/golang.mk

# ====================================================================================
# Setup Kubernetes tools

-include build/makelib/k8s_tools.mk

# Husky git hook manager tasks.
-include .husky/husky.mk

# ====================================================================================
# Setup Images

REGISTRY_ORGS ?= xpkg.upbound.io/linode
IMAGES = provider-ceph
-include build/makelib/imagelight.mk

# ====================================================================================
# Setup XPKG

XPKG_REG_ORGS ?= xpkg.upbound.io/linode
# NOTE(hasheddan): skip promoting on xpkg.upbound.io as channel tags are
# inferred.
XPKG_REG_ORGS_NO_PROMOTE ?= xpkg.upbound.io/linode
XPKGS = provider-ceph
-include build/makelib/xpkg.mk
-include build/makelib/local.xpkg.mk

# NOTE(hasheddan): we force image building to happen prior to xpkg build so that
# we ensure image is present in daemon.
xpkg.build.provider-ceph: do.build.images

something:
	echo something: $GO_TEST_PARALLEL

fallthrough: submodules
	@echo Initial setup complete. Running make again . . .
	@make

# integration tests
e2e.run: test-integration

# Update kind node versions to be tested.
update-kind-nodes:
	LATEST_KIND_NODE=$(LATEST_KIND_NODE) ./hack/update-kind-nodes.sh

# Generate kuttl e2e tests.
generate-tests:
	TEST_KIND_NODES=$(TEST_KIND_NODES) REPO=$(REPO) ./hack/generate-tests.sh

# Run integration tests.
test-integration: $(KIND) $(KUBECTL) $(UP) $(HELM3)
	@$(INFO) running integration tests using kind $(KIND_VERSION)
	@KIND_NODE_IMAGE_TAG=${KIND_NODE_IMAGE_TAG} $(ROOT_DIR)/cluster/local/integration_tests.sh || $(FAIL)
	@$(OK) integration tests passed

# Update the submodules, such as the common build scripts.
submodules:
	@git submodule sync
	@git submodule update --init --recursive

# NOTE(hasheddan): the build submodule currently overrides XDG_CACHE_HOME in
# order to force the Helm 3 to use the .work/helm directory. This causes Go on
# Linux machines to use that directory as the build cache as well. We should
# adjust this behavior in the build submodule because it is also causing Linux
# users to duplicate their build cache, but for now we just make it easier to
# identify its location in CI so that we cache between builds.
go.cachedir:
	@go env GOCACHE

# NOTE(hasheddan): we must ensure up is installed in tool cache prior to build
# as including the k8s_tools machinery prior to the xpkg machinery sets UP to
# point to tool cache.
build.init: $(UP)

# This is for running out-of-cluster locally, and is for convenience. Running
# this make target will print out the command which was used. For more control,
# try running the binary directly with different arguments.
run: go.build
	@$(INFO) Running Crossplane locally out-of-cluster . . .
	@# To see other arguments that can be provided, run the command with --help instead
	@# TODO: Webhooks are not enabled for local run.
	@# A workaround for tls certs is required.
	$(GO_OUT_DIR)/provider --zap-devel

# Spin up a Kind cluster and localstack.
# Create k8s service to allows pods to communicate with
# localstack.
cluster: $(KIND) $(KUBECTL) $(COMPOSE) cluster-clean
	@$(INFO) Creating localstack
	@$(COMPOSE) -f e2e/localstack/docker-compose.yml up -d
	@$(OK) Creating localstack
	@$(INFO) Creating kind cluster
	@$(KIND) create cluster --name=$(KIND_CLUSTER_NAME)
	@$(OK) Creating kind cluster
	@$(INFO) Creating Localstack Service
	@$(KUBECTL) apply -R -f e2e/localstack/service.yaml
	@$(OK) Creating Localstack Service

# Spin up a Kind cluster and localstack and install Crossplane via Helm.
crossplane-cluster: $(HELM3) cluster
	@$(INFO) Installing Crossplane
	@$(HELM3) repo add crossplane-stable https://charts.crossplane.io/stable
	@$(HELM3) repo update
	@$(HELM3) install crossplane --namespace crossplane-system --create-namespace crossplane-stable/crossplane
	@$(OK) Installing Crossplane

# Build the controller image and the provider package.
# Load the controller image to the Kind cluster and add the provider package
# to the Provider.
# The following is taken from local.xpkg.deploy.provider.
# However, it is modified to use the "--zap-devel" flag instead of "-d" which does
# not exist in this project and would therefore cause the controller to CrashLoop.
load-package: $(KIND) build
	@$(MAKE) local.xpkg.sync
	@$(INFO) deploying provider package $(PROJECT_NAME)
	@$(KIND) load docker-image $(BUILD_REGISTRY)/$(PROJECT_NAME)-$(ARCH) -n $(KIND_CLUSTER_NAME)
	@echo '{"apiVersion":"pkg.crossplane.io/v1alpha1","kind":"ControllerConfig","metadata":{"name":"config"},"spec":{"args":["--zap-devel","--enable-validation-webhooks", "--kube-client-rate=100000", "--reconcile-timeout=2s", "--max-reconcile-rate=5000", "--reconcile-concurrency=100", "--poll=30m", "--sync=1h"],"image":"$(BUILD_REGISTRY)/$(PROJECT_NAME)-$(ARCH)"}}' | $(KUBECTL) apply -f -
	@echo '{"apiVersion":"pkg.crossplane.io/v1","kind":"Provider","metadata":{"name":"$(PROJECT_NAME)"},"spec":{"package":"$(PROJECT_NAME)-$(VERSION).gz","packagePullPolicy":"Never","controllerConfigRef":{"name":"config"}}}' | $(KUBECTL) apply -f -
	@$(OK) deploying provider package $(PROJECT_NAME) $(VERSION)

# Spin up a Kind cluster and localstack and install Crossplane via Helm.
# Build the controller image and the provider package.
# Load the controller image to the Kind cluster and add the provider package
# to the Provider.
# Run Kuttl test suite on newly built controller image.
# Destroy Kind and localstack.
kuttl: $(KUTTL) crossplane-cluster load-package
	@$(INFO) Running kuttl test suite
	@$(KUTTL) test --config e2e/kuttl/stable/provider-ceph-1.27.yaml
	@$(OK) Running kuttl test suite
	@$(MAKE) cluster-clean

ceph-kuttl: $(KIND) $(KUTTL) $(HELM3) cluster-clean
	@$(INFO) Creating kind cluster
	@$(KIND) create cluster --name=$(KIND_CLUSTER_NAME)
	@$(OK) Creating kind cluster
	@$(KUTTL) test --config e2e/kuttl/ceph/provider-ceph-1.27.yaml

# Spin up a Kind cluster and localstack and install Crossplane CRDs (not
# containerised Crossplane componenets).
# Install local provider-ceph CRDs.
# Create ProviderConfig CR representing localstack.
dev-cluster: $(KUBECTL) cluster
	@$(INFO) Installing CRDs and ProviderConfig
	@$(KUBECTL) apply -k https://github.com/crossplane/crossplane//cluster?ref=master
	@$(KUBECTL) apply -R -f package/crds
	@# TODO: apply package/webhookconfigurations when webhooks can be enabled locally.
	@$(KUBECTL) apply -R -f e2e/localstack/localstack-provider-cfg.yaml
	@$(OK) Installing CRDs and ProviderConfig

# Best for development - locally run provider-ceph controller.
# Removes need for Crossplane install via Helm.
dev: dev-cluster run

# Destroy Kind cluster and localstack.
cluster-clean: $(KIND) $(KUBECTL) $(COMPOSE)
	@$(INFO) Deleting kind cluster
	@$(KIND) delete cluster --name=$(KIND_CLUSTER_NAME)
	@$(OK) Deleting kind cluster
	@$(INFO) Tearing down localstack
	@$(COMPOSE) -f e2e/localstack/docker-compose.yml stop
	@$(OK) Tearing down localstack

.PHONY: submodules fallthrough test-integration run cluster dev-cluster dev cluster-clean

# ====================================================================================
# Special Targets

# Install gomplate
GOMPLATE_VERSION := 3.10.0
GOMPLATE := $(TOOLS_HOST_DIR)/gomplate-$(GOMPLATE_VERSION)

$(GOMPLATE):
	@$(INFO) installing gomplate $(SAFEHOSTPLATFORM)
	@mkdir -p $(TOOLS_HOST_DIR)
	@curl -fsSLo $(GOMPLATE) https://github.com/hairyhenderson/gomplate/releases/download/v$(GOMPLATE_VERSION)/gomplate_$(SAFEHOSTPLATFORM) || $(FAIL)
	@chmod +x $(GOMPLATE)
	@$(OK) installing gomplate $(SAFEHOSTPLATFORM)

export GOMPLATE

# This target prepares repo for your provider by replacing all "ceph"
# occurrences with your provider name.
# This target can only be run once, if you want to rerun for some reason,
# consider stashing/resetting your git state.
# Arguments:
#   provider: Camel case name of your provider, e.g. GitHub, PlanetScale
provider.prepare:
	@[ "${provider}" ] || ( echo "argument \"provider\" is not set"; exit 1 )
	@PROVIDER=$(provider) ./hack/helpers/prepare.sh

# This target adds a new api type and its controller.
# You would still need to register new api in "apis/<provider>.go" and
# controller in "internal/controller/<provider>.go".
# Arguments:
#   provider: Camel case name of your provider, e.g. GitHub, PlanetScale
#   group: API group for the type you want to add.
#   kind: Kind of the type you want to add
#	apiversion: API version of the type you want to add. Optional and defaults to "v1alpha1"
provider.addtype: $(GOMPLATE)
	@[ "${provider}" ] || ( echo "argument \"provider\" is not set"; exit 1 )
	@[ "${group}" ] || ( echo "argument \"group\" is not set"; exit 1 )
	@[ "${kind}" ] || ( echo "argument \"kind\" is not set"; exit 1 )
	@PROVIDER=$(provider) GROUP=$(group) KIND=$(kind) APIVERSION=$(apiversion) ./hack/helpers/addtype.sh

define CROSSPLANE_MAKE_HELP
Crossplane Targets:
    submodules            Update the submodules, such as the common build scripts.
    run                   Run crossplane locally, out-of-cluster. Useful for development.

endef
# The reason CROSSPLANE_MAKE_HELP is used instead of CROSSPLANE_HELP is because the crossplane
# binary will try to use CROSSPLANE_HELP if it is set, and this is for something different.
export CROSSPLANE_MAKE_HELP

crossplane.help:
	@echo "$$CROSSPLANE_MAKE_HELP"

help-special: crossplane.help

.PHONY: crossplane.help help-special aws

# Install Docker Compose to run localstack.
COMPOSE ?= $PWD/bin/docker-compose
compose:
ifeq (,$(wildcard $(COMPOSE)))
	curl -sL https://github.com/docker/compose/releases/download/v2.17.3/docker-compose-linux-x86_64 -o $(COMPOSE)
	chmod +x $(COMPOSE)
endif

# Install aws cli for testing.
AWS ?= /usr/local/bin/aws
aws:
ifeq (,$(wildcard $(AWS)))
	curl https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip -o $PWD/bin/awscliv2.zip
	unzip $PWD/bin/awscliv2.zip -d $PWD/bin/
	$PWD/bin/aws/install
endif
