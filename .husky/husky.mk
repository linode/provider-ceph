HUSKY_VERSION ?= v0.2.16
HUSKY ?= $(TOOLS_HOST_DIR)/husky-$(HUSKY_VERSION)

husky.install: $(HUSKY)
	@$(HUSKY) install

## Download husky locally if necessary.
$(HUSKY):
	@$(INFO) installing husky $(HUSKY_VERSION)
	@mkdir -p $(TOOLS_HOST_DIR)
	@GOBIN=$(TOOLS_HOST_DIR) go install github.com/automation-co/husky@$(HUSKY_VERSION)
	@mv $(TOOLS_HOST_DIR)/husky $(HUSKY)
	@$(OK) installing husky $(HUSKY_VERSION)
