#!/bin/bash -e

: ${SOURCE?= required}
: ${KIND_CLUSTER_NAME?= required}

if [[ "$SOURCE" == https://* ]]; then
    MANIFESTS=$(curl -Ls $SOURCE)
elif [[ "$SOURCE" == file://* ]]; then
    MANIFESTS="$(cat "${SOURCE/file:\/\//""}")"
else
    MANIFESTS="$(eval "$SOURCE")"
fi

for img in `echo "${MANIFESTS}" | grep 'image: ' | sed 's/.*image://' | uniq | tr -d \"`; do
    docker pull ${img}
	kind load docker-image --name=${KIND_CLUSTER_NAME} ${img}
done
