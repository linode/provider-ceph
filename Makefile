# ====================================================================================
# Setup Project
PROJECT_NAME := provider-ceph
PROJECT_REPO := github.com/linode/$(PROJECT_NAME)
PROJECT_ROOT = $(shell git rev-parse --show-toplevel)

PLATFORMS ?= linux_amd64 linux_arm64
-include build/makelib/common.mk

# Generate chainsaw e2e tests for the following kind node versions
# TEST_KIND_NODES is not intended to be updated manually.
# Please edit LATEST_KIND_NODE instead and run 'make update-kind-nodes'.
TEST_KIND_NODES ?= 1.31.12,1.32.8,1.33.4,1.34.0
KIND_VERSION ?= v0.30.0
LATEST_KUBE_VERSION ?= 1.34
LATEST_KIND_NODE ?= 1.34.1
KUBECTL_VERSION ?= v1.34.0
REPO ?= provider-ceph

CROSSPLANE_CLI_VERSION ?= v2.0.2
CROSSPLANE_NAMESPACE ?= crossplane-system
CROSSPLANE_VERSION ?= 2.0.2
LOCALSTACK_VERSION ?= 4.7
CERT_MANAGER_VERSION ?= 1.14.0

# For local development
ASSUME_ROLE_ARN ?= "arn:akamai:sts:::assumed-role/TestRole"

# For local development, usually IP of docker0.
WEBHOOK_HOST ?= 172.17.0.1
# For local development, port of localtunnel.
WEBHOOK_TUNNEL_PORT ?= 9999
# For local development, subdomain of locatunnel.
WEBHOOK_SUBDOMAIN ?= $(PROJECT_NAME)-$(shell git rev-parse --short HEAD)-$(shell date +%s)

WEBHOOK_TYPE ?= stock

# Set this value to enforce use of Helm v3.
USE_HELM3 ?= true

# ====================================================================================
# Setup Output

-include build/makelib/output.mk

# ====================================================================================
# Setup Go

GO_STATIC_PACKAGES = $(GO_PROJECT)/cmd/provider
GO_LDFLAGS += -X $(GO_PROJECT)/internal/version.Version=$(VERSION)
GO_SUBDIRS += cmd internal apis
GO111MODULE = on
GOLANGCILINT_VERSION := $(shell grep 'GOLANGCI_VERSION' .github/workflows/ci.yml | sed -n 's/.*: *["'\'']*v\([0-9]\+\.[0-9]\+\.[0-9]\+\).*/\1/p')

-include build/makelib/golang.mk

# ====================================================================================
# Setup Kubernetes tools

-include build/makelib/k8s_tools.mk

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
XPKG_REG_ORGS_NO_PROMOTE ?= xpkg.upbound.io/linode xpkg.crossplane.internal/dev
XPKGS = provider-ceph
-include build/makelib/xpkg.mk
-include build/makelib/local.xpkg.mk

VAL_WBHK_STAGE ?= $(ROOT_DIR)/staging/validatingwebhookconfiguration

# ====================================================================================
# Setup Controlplane and E2E Testing (Crossplane v2)

-include build/makelib/controlplane.mk

# NOTE(hasheddan): we force image building to happen prior to xpkg build so that
# we ensure image is present in daemon.
xpkg.build.provider-ceph: do.build.images

fallthrough: submodules
	@echo Initial setup complete. Running make again . . .
	@make

# Update kind node versions to be tested.
update-kind-nodes:
	LATEST_KIND_NODE=$(LATEST_KIND_NODE) ./hack/update-kind-nodes.sh

# Generate chainsaw e2e tests.
generate-tests:
	TEST_KIND_NODES=$(TEST_KIND_NODES) REPO=$(REPO) LOCALSTACK_VERSION=$(LOCALSTACK_VERSION) CERT_MANAGER_VERSION=$(CERT_MANAGER_VERSION) ./hack/generate-tests.sh

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

build.init: $(CROSSPLANE_CLI)

# Export build variables for integration tests
build.vars: common.buildvars k8s_tools.buildvars
	@echo CROSSPLANE_VERSION=$(CROSSPLANE_VERSION)
	@echo CROSSPLANE_NAMESPACE=$(CROSSPLANE_NAMESPACE)
	@echo CROSSPLANE_CLI_VERSION=$(CROSSPLANE_CLI_VERSION)
	@echo KIND_VERSION=$(KIND_VERSION)
	@echo KUBECTL_VERSION=$(KUBECTL_VERSION)
	@echo LOCALSTACK_VERSION=$(LOCALSTACK_VERSION)
	@echo CERT_MANAGER_VERSION=$(CERT_MANAGER_VERSION)

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
	@KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) SOURCE="file://$(PWD)/e2e/localstack/localstack-deployment.yaml" ./hack/load-images.sh
	@$(KUBECTL) apply -R -f e2e/localstack/localstack-deployment.yaml

# Internal helper target to install Crossplane via Helm with configurable activations
# Usage: make _install-crossplane CROSSPLANE_ACTIVATIONS="<value>"
# CROSSPLANE_ACTIVATIONS can be empty (for default) or "provider.defaultActivations={}"
.PHONY: _install-crossplane
_install-crossplane: $(HELM)
	@$(INFO) Installing Crossplane $(CROSSPLANE_VERSION)
	@$(HELM) repo add crossplane-stable https://charts.crossplane.io/stable --force-update
	@$(HELM) repo update
	@$(HELM) get notes -n $(CROSSPLANE_NAMESPACE) crossplane >/dev/null 2>&1 || $(HELM) install crossplane --create-namespace --namespace=$(CROSSPLANE_NAMESPACE) --version $(CROSSPLANE_VERSION) $(CROSSPLANE_ACTIVATIONS) --set packageCache.sizeLimit=128Mi crossplane-stable/crossplane
	@$(OK) Crossplane installed

# Spin up a Kind cluster and install Crossplane 2.0+ with safe-start (empty activations)
crossplane-cluster: $(HELM) cluster
	@$(INFO) Installing Crossplane with safe-start support
	@KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) SOURCE="helm template crossplane --namespace crossplane-system --version $(CROSSPLANE_VERSION) crossplane-stable/crossplane" ./hack/load-images.sh
	@$(MAKE) _install-crossplane CROSSPLANE_ACTIVATIONS="--set provider.defaultActivations={}"

## Deploy cert manager to the K8s cluster specified in ~/.kube/config.
cert-manager: $(KUBECTL)
	@KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) SOURCE="https://github.com/cert-manager/cert-manager/releases/download/v$(CERT_MANAGER_VERSION)/cert-manager.yaml" ./hack/load-images.sh
	@$(KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/download/v$(CERT_MANAGER_VERSION)/cert-manager.yaml

# Generate the provider-ceph package and webhookconfiguration manifest.
generate-pkg: generate kustomize-webhook

# Ensure your branch/PR is ready for review. This target is used instead of the build submodule's
# 'reviewable' target. The only difference is that this target calls 'generate-pkg' instead of 'generate'
# to ensure we don't edit the webhook manifest and leave unmerged edits on the branch.
ready-for-review:
	@$(INFO) Ensuring branch is ready for review
	@$(MAKE) generate-pkg
	@$(MAKE) lint
	@$(MAKE) test
	@$(OK) Ensuring branch is ready for review

# Ensure generate-pkg target doesn't create a diff
check-diff-pkg: generate-pkg
	@$(INFO) checking that branch is clean
	@if git status --porcelain | grep . ; then $(ERR) There are uncommitted changes after running make generate-pkg. Please ensure you commit all generated files in this branch after running make generate-pkg. && false; else $(OK) branch is clean; fi

# Kustomize the webhookconfiguration manifest that is created by 'generate' target.
kustomize-webhook: $(KUSTOMIZE)
	@cp -f $(XPKG_DIR)/webhookconfigurations/manifests.yaml $(VAL_WBHK_STAGE)
	@cp -f $(VAL_WBHK_STAGE)/service-patch-$(WEBHOOK_TYPE).yaml $(VAL_WBHK_STAGE)/service-patch.yaml
	$(KUSTOMIZE) build $(VAL_WBHK_STAGE) -o $(XPKG_DIR)/webhookconfigurations/manifests.yaml

# Setup Crossplane with default activations for standard testing
.PHONY: crossplane-standard
crossplane-standard: $(KIND) $(KUBECTL)
	@$(INFO) Setting up kind cluster for standard testing
	@$(KIND) get kubeconfig --name $(KIND_CLUSTER_NAME) >/dev/null 2>&1 || $(KIND) create cluster --name=$(KIND_CLUSTER_NAME)
	@$(INFO) Setting kubectl context to kind-$(KIND_CLUSTER_NAME)
	@$(KUBECTL) config use-context "kind-$(KIND_CLUSTER_NAME)"
	@$(MAKE) _install-crossplane CROSSPLANE_ACTIVATIONS=""

# Setup Crossplane for safe-start testing
.PHONY: crossplane-safe-start
crossplane-safe-start: $(KIND) $(KUBECTL)
	@$(INFO) Setting up kind cluster for safe-start testing
	@$(KIND) get kubeconfig --name $(KIND_CLUSTER_NAME) >/dev/null 2>&1 || $(KIND) create cluster --name=$(KIND_CLUSTER_NAME)
	@$(INFO) Setting kubectl context to kind-$(KIND_CLUSTER_NAME)
	@$(KUBECTL) config use-context "kind-$(KIND_CLUSTER_NAME)"
	@$(MAKE) _install-crossplane CROSSPLANE_ACTIVATIONS="--set provider.defaultActivations={}"

# Run Chainsaw test suite for safe-start testing
.PHONY: chainsaw-safe-start
chainsaw-safe-start: $(CHAINSAW) build generate-pkg generate-tests
	@$(INFO) Running chainsaw safe-start test suite with Crossplane v2
	@$(MAKE) crossplane-safe-start
	@$(MAKE) localstack-cluster
	@$(MAKE) local.xpkg.deploy.provider.$(PROJECT_NAME)
	@$(CHAINSAW) test e2e/tests/safe-start --config e2e/tests/safe-start/.chainsaw.yaml
	@$(OK) Running chainsaw safe-start test suite
	@$(MAKE) controlplane.down

# Run Chainsaw test suite using build submodule's controlplane infrastructure
# This is the standardized approach for Crossplane v2 testing
.PHONY: chainsaw
chainsaw: $(CHAINSAW) build generate-pkg generate-tests
	@$(INFO) Running chainsaw test suite with Crossplane v2
	@$(MAKE) crossplane-standard
	@$(MAKE) localstack-cluster
	@$(MAKE) local.xpkg.deploy.provider.$(PROJECT_NAME)
	@$(CHAINSAW) test e2e/tests/stable --config e2e/tests/stable/.chainsaw.yaml
	@$(OK) Running chainsaw test suite
	@$(MAKE) controlplane.down

.PHONY: ceph-chainsaw
ceph-chainsaw: $(CHAINSAW) build generate-pkg
	@$(INFO) Running chainsaw test suite against ceph cluster
	@$(MAKE) controlplane.up
	@$(MAKE) local.xpkg.deploy.provider.$(PROJECT_NAME)
	@$(CHAINSAW) test e2e/tests/ceph || ($(MAKE) controlplane.down && $(FAIL))
	@$(OK) Running chainsaw test suite against ceph cluster
	@$(MAKE) controlplane.down

# Spin up a Kind cluster and localstack and install Crossplane CRDs (not
# containerised Crossplane componenets).
# Install local provider-ceph CRDs.
# Create ProviderConfig CR representing localstack.
dev-cluster: $(KUBECTL) generate-pkg generate-tests crossplane-cluster localstack-cluster cert-manager
	@$(INFO) Rendering webhook manifest.
	@$(MAKE) render-webhook
	@$(INFO) Installing CRDs, ProviderConfig and Localstack
	@$(KUBECTL) apply -R -f package/crds
	@$(KUBECTL) apply -R -f package/webhookconfigurations
	@$(KUBECTL) apply -R -f e2e/localstack/localstack-provider-cfg-host.yaml
	@$(OK) Installing CRDs and ProviderConfig
	@$(MAKE) webhook-tunnel

# Spin up a Kind cluster install Crossplane CRDs (not containerised Crossplane componenets).
# Install local provider-ceph CRDs.
# Create ProviderConfig CR representing your own Ceph cluster.
dev-cluster-ceph: $(KUBECTL) generate-pkg generate-tests crossplane-cluster localstack-cluster cert-manager
	@$(INFO) Rendering webhook manifest.
	@$(MAKE) render-webhook
	@$(INFO) Installing CRDs, ProviderConfig and Localstack
	@$(KUBECTL) apply -R -f package/crds
	@$(KUBECTL) apply -R -f package/webhookconfigurations
	@$(OK) Installing CRDs and ProviderConfig
	@$(MAKE) webhook-tunnel
	@AWS_ACCESS_KEY_ID=$(AWS_ACCESS_KEY_ID) AWS_SECRET_ACCESS_KEY=$(AWS_SECRET_ACCESS_KEY) CEPH_ADDRESS=$(CEPH_ADDRESS) ./hack/install-pc-ceph-cluster.sh

render-webhook:
	@$(INFO) Rendering webhook manifest.
	@ex $(VAL_WBHK_STAGE)/service-patch-dev.tpl.yaml \
		-c '%s/#WEBHOOK_HOST#/$(WEBHOOK_SUBDOMAIN).loca.lt/' \
		-c 'sav! $(VAL_WBHK_STAGE)/service-patch.yaml' \
		-c 'q' >/dev/null
	$(KUSTOMIZE) build $(VAL_WBHK_STAGE) -o $(XPKG_DIR)/webhookconfigurations/manifests.yaml
	@$(OK) Rendering webhook manifest.

webhook-tunnel:
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

# Locally run provider-ceph controller against your own Ceph Cluster.
# Removes need for Crossplane install via Helm.
dev-ceph: dev-cluster-ceph run

# Destroy Kind cluster and localstack.
cluster-clean: $(KIND) $(KUBECTL)
	@$(INFO) Deleting kind cluster
	@$(KIND) delete cluster --name=$(KIND_CLUSTER_NAME)
	@$(OK) Deleting kind cluster

.PHONY: submodules fallthrough run cluster dev-cluster dev cluster-clean

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
	curl https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip -o $(PWD)/bin/awscliv2.zip
	unzip $(PWD)/bin/awscliv2.zip -d $(PWD)/bin/
	$(PWD)/bin/aws/install
endif

NILAWAY_VERSION ?= latest
NILAWAY := $(TOOLS_HOST_DIR)/nilaway-$(NILAWAY_VERSION)

.PHONY: nilcheck
nilcheck: $(NILAWAY) ## Run nil check against codemake.
	@# The bucket_backends.go is nil safe, covered by tests.
	@# Backendstore contains mostly nil safe generated files.
	@# Lifecycleconfig_helper has false positive reports: https://github.com/uber-go/nilaway/issues/207
	go list ./... | xargs -I {} -d '\n' $(NILAWAY) \
		-exclude-errors-in-files $(PWD)/cmd/provider/main.go,$(PWD)/internal/controller/bucket/bucket_backends.go,$(PWD)/internal/rgw/lifecycleconfig_helpers.go,$(PWD)/internal/rgw/objectlockconfiguration_helpers.go \
		-exclude-pkgs github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1,github.com/linode/provider-ceph/internal/backendstore \
		-include-pkgs {} ./...

# nilaway download and install.
$(NILAWAY):
	@$(INFO) installing nilaway $(NILAWAY_VERSION)
	@mkdir -p $(TOOLS_HOST_DIR)
	@GOBIN=$(TOOLS_HOST_DIR) go install go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION)
	@mv $(TOOLS_HOST_DIR)/nilaway $(NILAWAY)
	@$(OK) installing nilaway $(NILAWAY_VERSION)

GOVULNCHECK_VERSION ?= v1.1.4
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
