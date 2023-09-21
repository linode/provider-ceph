#!/bin/bash

set -e

VERSION=${1:?}
IMAGE=${2:-linode/provider-ceph}

for file in "package/crossplane.yaml" "README.md"
do
    sed -E -i \
        "s|${IMAGE}:(.*)|${IMAGE}:${VERSION}|g" \
        $file
done
