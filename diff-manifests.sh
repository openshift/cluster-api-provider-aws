#!/usr/bin/env bash

# This file is used to diff the manifests generated against what's provided in
# the cluster-capi-operator repository. It assumes that there is a clone of the
# cluster-capi-operator repo in the same directory as the cluster-api-provider-aws clone.

for F in $(ls manifests)
do
    echo "---> ${F}"
    diff manifests/${F} ../cluster-capi-operator/manifests/${F}
done
