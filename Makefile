# ====================================================================================
# Setup Project
PROJECT_NAME := provider-ceph
PROJECT_REPO := github.com/linode/$(PROJECT_NAME)
PROJECT_ROOT = $(shell git rev-parse --show-toplevel)

PLATFORMS ?= linux_amd64 linux_arm64
-include build/makelib/common.mk

# Generate kuttl e2e tests for the following kind node versions
# TEST_KIND_NODES is not intended to be updated manually.
# Please edit LATEST_KIND_NODE instead and run 'make update-kind-nodes'.
TEST_KIND_NODES ?= 1.26.14,1.27.11,1.28.7,1.29.2

LATEST_KUBE_VERSION ?= 1.29
LATEST_KIND_NODE ?= 1.29.2
REPO ?= provider-ceph

KUTTL_VERSION ?= 0.15.0

CROSSPLANE_VERSION ?= 1.15.0

# For local development
ASSUME_ROLE_ARN ?= "arn:akamai:sts:::assumed-role/TestRole"

# For local development, usually IP of docker0.
WEBHOOK_HOST ?= 172.17.0.1
# For local development, port of localtunnel.
WEBHOOK_TUNNEL_PORT ?= 9999
# For local development, subdomain of locatunnel.
WEBHOOK_SUBDOMAIN ?= $(PROJECT_NAME)-$(shell git rev-parse --short HEAD)-$(shell date +%s)

WEBHOOK_TYPE ?= stock

# ====================================================================================
# Setup Output

-include build/makelib/output.mk

# ====================================================================================
# Setup Go

GO_STATIC_PACKAGES = $(GO_PROJECT)/cmd/provider
GO_LDFLAGS += -X $(GO_PROJECT)/internal/version.Version=$(VERSION)
GO_SUBDIRS += cmd internal apis
GO111MODULE = on
GOLANGCILINT_VERSION ?= 1.56.2
-include build/makelib/golang.mk

# ====================================================================================
# Setup Kubernetes tools

-include build/makelib/k8s_tools.mk

# Husky git hook manager tasks.
-include .husky/husky.mk

# Mirrord local dev tasks.
-include .mirrord/mirrord.mk

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

VAL_WBHK_STAGE ?= $(ROOT_DIR)/staging/validatingwebhookconfiguration

# NOTE(hasheddan): we force image building to happen prior to xpkg build so that
# we ensure image is present in daemon.
xpkg.build.provider-ceph: do.build.images

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
	$(GO_OUT_DIR)/provider --zap-devel --webhook-tls-cert-dir=$(PWD)/bin/certs --webhook-host=$(WEBHOOK_HOST)

# Spin up a Kind cluster.
cluster: $(KIND) $(KUBECTL) cluster-clean
	@$(INFO) Creating kind cluster
	@$(KIND) create cluster --name=$(KIND_CLUSTER_NAME) --config e2e/kind/kind-config-$(LATEST_KUBE_VERSION).yaml
	@$(OK) Creating kind cluster

# Spin up localstack cluster.
localstack-cluster: $(KIND) $(KUBECTL)
	docker pull localstack/localstack:2.2
	@$(KIND) load docker-image --name=$(KIND_CLUSTER_NAME) localstack/localstack:2.2
	@$(KUBECTL) apply -R -f e2e/localstack/localstack-deployment.yaml

# Spin up a Kind cluster and install Crossplane via Helm.
crossplane-cluster: $(HELM3) cluster
	@$(INFO) Installing Crossplane
	@$(HELM3) repo add crossplane-stable https://charts.crossplane.io/stable
	@$(HELM3) repo update
	@$(HELM3) install crossplane --namespace crossplane-system --create-namespace --version $(CROSSPLANE_VERSION) crossplane-stable/crossplane
	@$(OK) Installing Crossplane

## Deploy cert manager to the K8s cluster specified in ~/.kube/config.
cert-manager: $(KUBECTL)
	@docker pull quay.io/jetstack/cert-manager-controller:v1.14.0
	@docker pull quay.io/jetstack/cert-manager-webhook:v1.14.0
	@docker pull quay.io/jetstack/cert-manager-cainjector:v1.14.0
	@$(KIND) load docker-image --name=$(KIND_CLUSTER_NAME) quay.io/jetstack/cert-manager-controller:v1.14.0
	@$(KIND) load docker-image --name=$(KIND_CLUSTER_NAME) quay.io/jetstack/cert-manager-webhook:v1.14.0
	@$(KIND) load docker-image --name=$(KIND_CLUSTER_NAME) quay.io/jetstack/cert-manager-cainjector:v1.14.0
	@$(KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml

# Generate the provider-ceph package and webhookconfiguration manifest.
generate-pkg: generate kustomize-webhook

# Ensure generate-pkg target doesn't create a diff
check-diff-pkg: generate-pkg
	@$(INFO) checking that branch is clean
	@if git status --porcelain | grep . ; then $(ERR) There are uncommitted changes after running make generate-pkg. Please ensure you commit all generated files in this branch after running make generate-pkg. && false; else $(OK) branch is clean; fi

# Kustomize the webhookconfiguration manifest that is created by 'generate' target.
kustomize-webhook: $(KUSTOMIZE)
	@cp -f $(XPKG_DIR)/webhookconfigurations/manifests.yaml $(VAL_WBHK_STAGE)
	@cp -f $(VAL_WBHK_STAGE)/service-patch-$(WEBHOOK_TYPE).yaml $(VAL_WBHK_STAGE)/service-patch.yaml
	$(KUSTOMIZE) build $(VAL_WBHK_STAGE) -o $(XPKG_DIR)/webhookconfigurations/manifests.yaml

# Build the controller image and the provider package.
# Load the controller image to the Kind cluster and add the provider package
# to the Provider.
# The following is taken from local.xpkg.deploy.provider.
# However, it is modified to use the "--zap-devel" flag instead of "-d" which does
# not exist in this project and would therefore cause the controller to CrashLoop.
load-package: $(KIND) build kustomize-webhook
	@$(MAKE) local.xpkg.sync
	@$(INFO) deploying provider package $(PROJECT_NAME)
	@$(KIND) load docker-image $(BUILD_REGISTRY)/$(PROJECT_NAME)-$(ARCH) -n $(KIND_CLUSTER_NAME)
	@BUILD_REGISTRY=$(BUILD_REGISTRY) PROJECT_NAME=$(PROJECT_NAME) ARCH=$(ARCH) VERSION=$(VERSION) ./hack/deploy-provider.sh
	@$(OK) deploying provider package $(PROJECT_NAME) $(VERSION)

# Spin up a Kind cluster and localstack and install Crossplane via Helm.
# Build the controller image and the provider package.
# Load the controller image to the Kind cluster and add the provider package
# to the Provider.
# Run Kuttl test suite on newly built controller image.
# Destroy Kind and localstack.
kuttl: $(KUTTL) generate-pkg generate-tests crossplane-cluster localstack-cluster cert-manager load-package
	@$(INFO) Running kuttl test suite
	@$(KUTTL) test --config e2e/kuttl/stable/provider-ceph-$(LATEST_KUBE_VERSION).yaml
	@$(OK) Running kuttl test suite
	@$(MAKE) cluster-clean

ceph-kuttl: $(KIND) $(KUTTL) $(HELM3) cluster-clean
	@$(INFO) Creating kind cluster
	@$(KIND) create cluster --name=$(KIND_CLUSTER_NAME)
	@$(OK) Creating kind cluster
	@$(KUTTL) test --config e2e/kuttl/ceph/provider-ceph-$(LATEST_KUBE_VERSION).yaml

# Spin up a Kind cluster and localstack and install Crossplane CRDs (not
# containerised Crossplane componenets).
# Install local provider-ceph CRDs.
# Create ProviderConfig CR representing localstack.
dev-cluster: $(KUBECTL) generate-pkg generate-tests crossplane-cluster localstack-cluster cert-manager
	@$(INFO) Rendering webhook manifest.
	@ex $(VAL_WBHK_STAGE)/service-patch-dev.tpl.yaml \
		-c '%s/#WEBHOOK_HOST#/$(WEBHOOK_SUBDOMAIN).loca.lt/' \
		-c 'sav! $(VAL_WBHK_STAGE)/service-patch.yaml' \
		-c 'q' >/dev/null
	$(KUSTOMIZE) build $(VAL_WBHK_STAGE) -o $(XPKG_DIR)/webhookconfigurations/manifests.yaml
	@$(OK) Rendering webhook manifest.

	@$(INFO) Installing CRDs, ProviderConfig and Localstack
	@$(KUBECTL) apply -R -f package/crds
	@$(KUBECTL) apply -R -f package/webhookconfigurations
	@$(KUBECTL) apply -R -f e2e/localstack/localstack-provider-cfg-host.yaml
	@$(OK) Installing CRDs and ProviderConfig

	@$(INFO) Starting webhook tunnel.
	@WEBHOOK_TUNNEL_PORT=$(WEBHOOK_TUNNEL_PORT) WEBHOOK_HOST=$(WEBHOOK_HOST) WEBHOOK_SUBDOMAIN=$(WEBHOOK_SUBDOMAIN) docker compose -f ./hack/localtunnel.yaml up -d
	@$(OK) Starting webhook tunnel.
	
	@$(INFO) Generate TLS certificate for webhook.
	@$(MAKE) dev-cluster-cacert
	@openssl genrsa \
		-out $(PWD)/bin/certs/tls.key \
		1024
	@openssl req \
		-new \
		-key $(PWD)/bin/certs/tls.key \
		-out $(PWD)/bin/certs/tls.csr \
		-subj "/CN=linode.com"
	@openssl x509 \
		-req \
		-days 365 \
		-in $(PWD)/bin/certs/tls.csr \
		-out $(PWD)/bin/certs/tls.crt \
		-extfile <(printf "subjectAltName = IP:$(WEBHOOK_HOST)") \
		-CAcreateserial \
		-CA $(PWD)/bin/certs/ca.crt \
		-CAkey $(PWD)/bin/certs/ca.key
	@$(OK) Generate TLS certificate for webhook.

dev-cluster-cacert: $(KUBECTL)
	@rm -rf $(PWD)/bin/certs ; mkdir $(PWD)/bin/certs
	@docker cp local-dev-control-plane:/etc/kubernetes/pki/ca.key $(PWD)/bin/certs/ca.key
	@docker cp local-dev-control-plane:/etc/kubernetes/pki/ca.crt $(PWD)/bin/certs/ca.crt
	@chmod 400 $(PWD)/bin/certs/*.crt

# Best for development - locally run provider-ceph controller.
# Removes need for Crossplane install via Helm.
dev: dev-cluster run

# Destroy Kind cluster and localstack.
cluster-clean: $(KIND) $(KUBECTL)
	@$(INFO) Deleting kind cluster
	@$(KIND) delete cluster --name=$(KIND_CLUSTER_NAME)
	@$(OK) Deleting kind cluster

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
    submodules      Update the submodules, such as the common build scripts.
    run             Run crossplane locally, out-of-cluster. Useful for development.
    generate-pkg    Generate the provider-ceph package and webhook configuration manifest.
    check-diff-pkg  Ensure the reviewable target from generate-pkg doesn't create a git diff.

endef
# The reason CROSSPLANE_MAKE_HELP is used instead of CROSSPLANE_HELP is because the crossplane
# binary will try to use CROSSPLANE_HELP if it is set, and this is for something different.
export CROSSPLANE_MAKE_HELP

crossplane.help:
	@echo "$$CROSSPLANE_MAKE_HELP"

help-special: crossplane.help

.PHONY: crossplane.help help-special aws

# Install aws cli for testing.
AWS ?= /usr/local/bin/aws
aws:
ifeq (,$(wildcard $(AWS)))
	curl https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip -o $PWD/bin/awscliv2.zip
	unzip $PWD/bin/awscliv2.zip -d $PWD/bin/
	$PWD/bin/aws/install
endif

NILAWAY_VERSION ?= latest
NILAWAY := $(TOOLS_HOST_DIR)/nilaway-$(NILAWAY_VERSION)

.PHONY: nilcheck
nilcheck: $(NILAWAY) ## Run nil check against codemake.
	@# The bucket_backends.go is nil safe, covered by tests.
	@# Backendstore contains mostly nil safe generated files.
	@# Lifecycleconfig_helper has false positive reports: https://github.com/uber-go/nilaway/issues/207
	go list ./... | xargs -I {} -d '\n' $(NILAWAY) \
		-exclude-errors-in-files $(PWD)/internal/controller/bucket/bucket_backends.go,$(PWD)/internal/rgw/lifecycleconfig_helpers.go \
		-exclude-pkgs github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1,github.com/linode/provider-ceph/internal/backendstore \
		-include-pkgs {} ./...

# nilaway download and install.
$(NILAWAY):
	@$(INFO) installing nilaway $(NILAWAY_VERSION)
	@mkdir -p $(TOOLS_HOST_DIR)
	@GOBIN=$(TOOLS_HOST_DIR) go install go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION)
	@mv $(TOOLS_HOST_DIR)/nilaway $(NILAWAY)
	@$(OK) installing nilaway $(NILAWAY_VERSION)

GOVULNCHECK_VERSION ?= v1.0.4
GOVULNCHECK := $(TOOLS_HOST_DIR)/govulncheck-$(GOVULNCHECK_VERSION)

.PHONY: vulncheck
vulncheck: $(GOVULNCHECK) ## Run vulnerability check against code.
	$(GOVULNCHECK) ./...

# govulncheck download and install.
$(GOVULNCHECK):
	@$(INFO) installing govulncheck $(GOVULNCHECK_VERSION)
	@mkdir -p $(TOOLS_HOST_DIR)
	@GOBIN=$(TOOLS_HOST_DIR) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@mv $(TOOLS_HOST_DIR)/govulncheck $(GOVULNCHECK)
	@$(OK) installing govulncheck $(GOVULNCHECK_VERSION)
