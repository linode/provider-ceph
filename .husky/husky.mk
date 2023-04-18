
HUSKY ?= $(shell pwd)/bin/husky
.PHONY: husky
husky: ## Download husky locally if necessary.
	$(call go-get-tool,$(HUSKY),github.com/automation-co/husky@v0.2.14)

# go-get-tool will 'go get' any package $2 and install it to $1.
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(shell pwd)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef