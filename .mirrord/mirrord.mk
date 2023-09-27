MIRRORD_VERSION ?= 3.70.0
MIRRORD := $(TOOLS_HOST_DIR)/mirrord-$(MIRRORD_VERSION)

# mirrord download and install
$(MIRRORD):
	@$(INFO) installing mirrord $(MIRRORD_VERSION)
	@mkdir -p $(TOOLS_HOST_DIR) || $(FAIL)
	@curl -fsSLo $(MIRRORD) https://github.com/metalbear-co/mirrord/releases/download/$(MIRRORD_VERSION)/mirrord_$(HOST_PLATFORM:-=_) || $(FAIL)
	@chmod +x $(MIRRORD)
	@$(OK) installing mirrord $(MIRRORD_VERSION)
