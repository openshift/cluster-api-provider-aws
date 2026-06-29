#!/usr/bin/env bash

set -euo pipefail

oc get -f openshift/capi-operator-manifests/default/manifests.yaml

# TODO: Update the array below when https://redhat.atlassian.net/browse/OCPCLOUD-3537 is done
declare -a arr=("pod.not-exist.io" "svc.not-exist.io")

for crd in "${arr[@]}"; do
  echo "Checking if ${crd} exists on the cluster"
  crd_name="$(oc get crd "$crd" --ignore-not-found -o name)"
  if [[ -n "$crd_name" ]]; then
    >&2 echo "Error: found unexpected CRD ${crd}!"
    exit 1
  fi
done
