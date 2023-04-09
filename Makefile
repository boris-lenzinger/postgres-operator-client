
# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -O globstar -o pipefail
.SHELLFLAGS = -ec

GO ?= go
GO_BUILD ?= $(GO) build --trimpath
GO_TEST = $(GO) test

KUBE_CLIENT ?= kubectl
KUTTL_TEST ?= kuttl test

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as 'target: ## some-help-text', and then formatting the target and help.
# Each line beginning with '##@ some-text' is formatted as a category.

.PHONY: help
help: ALIGN=18
help: ## Display this help
	@awk ' \
		BEGIN { FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n" } \
		/^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-$(ALIGN)s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } \
	' $(MAKEFILE_LIST)

##@ Development

.PHONY: all
all: check build

bin/kubectl-pgo-%: go.* $(shell ls -1 cmd/**/*.go internal/**/*.go)
	GOOS=$(word 1,$(subst -, ,$*)) GOARCH=$(word 2,$(subst -, ,$*)) $(GO_BUILD) -o $@ ./cmd/kubectl-pgo

.PHONY: build
build: ## Build the executable (requires Go)
build: bin/kubectl-pgo-$(subst $(eval) ,-,$(shell $(GO) env GOOS GOARCH))
	ln -fs $(notdir $<) ./bin/kubectl-pgo

.PHONY: check
check:
	$(GO_TEST) -cover ./...

# Expects operator to be running
.PHONY: check-kuttl
check-kuttl: PATH := $(PWD)/bin:$(PATH)
check-kuttl:
	${KUBE_CLIENT} ${KUTTL_TEST} \
		--config testing/kuttl/kuttl-test.yaml

.PHONY: clean
clean: ## Remove files generated by other targets
	rm -f ./bin/kubectl-pgo ./bin/kubectl-pgo-*-*

.PHONY: cli-docs
cli-docs: ## generate cli documenation
	rm -rf docs/content/reference && mkdir docs/content/reference
	cd docs/content/reference && $(GO) run -tags docs -exec 'env HOME=$$HOME' ../../../hack/generate-docs.go
	NL=$$'\n'; sed -e "1,/---/ { /^title:/ { \
			a \\$${NL}aliases:\\$${NL}- /reference/pgo\\$${NL}weight: 100$${NL}; \
			c \\$${NL}title: Command Reference$${NL}; \
		}; }" \
		docs/content/reference/pgo.md > \
		docs/content/reference/_index.md
	rm docs/content/reference/pgo.md

.PHONY: check-cli-docs
check-cli-docs: cli-docs
	git diff --exit-code -- docs/content/reference/
