BIN_DIR := bin
TOOLS_DIR := tools

$(RELEASE_DIR):
	mkdir -p $(RELEASE_DIR)/

MANIFESTS_GEN := go run ./vendor/github.com/openshift/cluster-capi-operator/manifests-gen/
YQ := go run github.com/mikefarah/yq/v4@v4.48.1

.PHONY: check-env
check-env:
ifndef PROVIDER_VERSION
	$(error PROVIDER_VERSION is undefined)
endif

.PHONY: verify
verify: verify-ocp-manifests

# NOTE: This target extracts the provider version from the existing generated manifest
# and uses it for regeneration. This means the version label itself is NOT verified,
# only that the rest of the manifest structure is correct.
MANIFEST_FILE := manifests/0000_30_cluster-api_04_cm.infrastructure-aws.yaml
.PHONY: verify-ocp-manifests
verify-ocp-manifests:
	@echo "Extracting provider version from existing manifest..."
	$(eval PROVIDER_VERSION := $(shell ${YQ} eval '.metadata.labels."provider.cluster.x-k8s.io/version"' $(MANIFEST_FILE)))
	@if [ -z "$(PROVIDER_VERSION)" ]; then \
		echo "Error: Could not extract provider version from $(MANIFEST_FILE)"; \
		exit 1; \
	fi
	@echo "Using PROVIDER_VERSION=$(PROVIDER_VERSION) for verification"
	@$(MAKE) ocp-manifests PROVIDER_VERSION=$(PROVIDER_VERSION) || { \
		echo "Error: Failed to generate manifests"; \
		exit 1; \
	}
	@if ./verify-diff.sh; then \
		echo ""; \
		echo "Verification successful! Manifests are in sync."; \
	else \
		git restore $(MANIFEST_FILE); \
		echo ""; \
		echo "Verification failed! Manifests are out of sync."; \
		echo "Please run:"; \
		echo "  PROVIDER_VERSION=<release-version> make ocp-manifests"; \
		echo ""; \
		echo "Where <release-version> is the desired upstream release version tag (e.g., v2.9.2)"; \
		echo "that matches the upstream version the OpenShift fork is based on."; \
		exit 1; \
	fi

.PHONY: update-manifests-gen
update-manifests-gen:
	cd tools && go get github.com/openshift/cluster-capi-operator/manifests-gen && go mod tidy && go mod vendor

.PHONY: ocp-manifests
ocp-manifests: $(RELEASE_DIR) check-env ## Builds openshift specific manifests
	# Generate provider manifests.
	cd tools && $(MANIFESTS_GEN) --provider-name "aws" --provider-type "InfrastructureProvider" --provider-version "${PROVIDER_VERSION}" --base-path "../../" --manifests-path "../manifests"
