MIRRORD_VERSION ?= 3.88.0
MIRRORD := $(TOOLS_HOST_DIR)/mirrord-$(MIRRORD_VERSION)

# Best for development - locally run provider-ceph controller.
mirrord.cluster: generate-pkg generate-tests crossplane-cluster localstack-cluster cert-manager load-package mirrord.certs
	@$(KUBECTL) apply -R -f package/crds
	@$(KUBECTL) apply -R -f package/webhookconfigurations
	$(KUBECTL) apply -f $(PROJECT_ROOT)/e2e/localstack/localstack-provider-cfg-host.yaml

mirrord.certs: $(KUBECTL)
	@rm -rf $(PWD)/bin/certs ; mkdir $(PWD)/bin/certs
	@$(KUBECTL) get secret -n crossplane-system crossplane-provider-provider-ceph-server-cert -o 'go-template={{index .data "ca.crt"}}' | base64 -d >> $(PWD)/bin/certs/ca.crt
	@$(KUBECTL) get secret -n crossplane-system crossplane-provider-provider-ceph-server-cert -o 'go-template={{index .data "tls.crt"}}' | base64 -d >> $(PWD)/bin/certs/tls.crt
	@$(KUBECTL) get secret -n crossplane-system crossplane-provider-provider-ceph-server-cert -o 'go-template={{index .data "tls.key"}}' | base64 -d >> $(PWD)/bin/certs/tls.key
	@chmod 400 $(PWD)/bin/certs/*.crt

mirrord.run: $(MIRRORD)
	@$(INFO) Starting mirrord on deployment
	$(MIRRORD) exec -f .mirrord/mirrord.json make run

# Download mirrord locally if necessary.
$(MIRRORD):
	@$(INFO) installing mirrord $(MIRRORD_VERSION)
	@mkdir -p $(TOOLS_HOST_DIR) || $(FAIL)
	@curl -fsSLo $(MIRRORD) https://github.com/metalbear-co/mirrord/releases/download/$(MIRRORD_VERSION)/mirrord_$(HOST_PLATFORM:-=_) || $(FAIL)
	@chmod +x $(MIRRORD)
	@$(OK) installing mirrord $(MIRRORD_VERSION)
