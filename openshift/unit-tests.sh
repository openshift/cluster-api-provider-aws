#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

echo "Running unit-tests.sh"

# Ensure that some home var is set and that it's not the root.
export HOME=${HOME:=/tmp/kubebuilder/testing}
if [ "$HOME" == "/" ]; then
  export HOME=/tmp/kubebuilder/testing
fi

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
# shellcheck source=../hack/ensure-go.sh
source "${REPO_ROOT}/hack/ensure-go.sh"

# Override vendoring: the upstream repo does not vendor, but the OpenShift fork
# does. Tests depend on non-Go assets (e.g. CRD YAMLs) from the module cache
# that are not present in the vendor directory.
unset GOFLAGS
export GOFLAGS=-mod=mod

cd "${REPO_ROOT}" && make test-verbose
