BIN_DIR := bin
TOOLS_DIR := tools
REPO_OWNER ?= openshift
REPO_NAME ?= cluster-api-provider-aws
PULL_BASE_SHA ?= main

include provider-version.mk

MANIFESTS_GEN := go run ./vendor/github.com/openshift/cluster-capi-operator/manifests-gen/

.PHONY: verify
verify: verify-ocp-manifests verify-gh-summary

.PHONY: verify-%
verify-%: ## Ensure no diff after running some other target
	$(MAKE) $*
	./verify-diff.sh

.PHONY: gh-summary
gh-summary: gh-summary-default

.PHONY: gh-summary-%
gh-summary-%:
	curl --connect-timeout 10 --max-time 120 --retry 3 -fsSL "https://raw.githubusercontent.com/$(REPO_OWNER)/$(REPO_NAME)/$(PULL_BASE_SHA)/openshift/capi-operator-manifests/$*/manifests-summary.yaml" -o "capi-operator-manifests/$*/manifests-summary.yaml"

.PHONY: update-manifests-gen
update-manifests-gen:
	cd tools && go get github.com/openshift/cluster-capi-operator/manifests-gen && go mod tidy && go mod vendor

.PHONY: ocp-manifests
ocp-manifests: ## Builds openshift specific manifests
	# Generate provider manifests.
	cd tools && $(MANIFESTS_GEN) --manifests-path "../capi-operator-manifests" --kustomize-dir="../../openshift" \
		--name cluster-api-provider-aws \
		--install-order 20 \
		--attribute type=infrastructure \
		--attribute version="${PROVIDER_VERSION}" \
		--self-image-ref registry.ci.openshift.org/openshift:aws-cluster-api-controllers \
		--platform AWS \
		--protect-cluster-resource awscluster \
		--manifests-summary
