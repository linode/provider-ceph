#!/bin/bash

: "${TEST_KIND_NODES?= required}"
: "${REPO?= required}"
: "${LOCALSTACK_VERSION?= required}"

# This script reads a comma-delimited string TEST_KIND_NODES of kind node versions
# for chainsaw tests to be run on, and generates the relevant files for each version.

IFS=', ' read -r -a kind_nodes <<< "$TEST_KIND_NODES"


# remove existing files
rm -f ./e2e/kind/*

HEADER="# This file was auto-generated by hack/generate-tests.sh"

for kind_node in "${kind_nodes[@]}"
do
	# write kind config file for version
	major=${kind_node%.*}
	if [ ! -d "./e2e/kind" ]; then
		mkdir -p ./e2e/kind
	fi
	file=./e2e/kind/kind-config-${major}.yaml

	cat <<EOF > "${file}"
${HEADER}
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: kindest/node:v${kind_node}
  extraPortMappings:
  - containerPort: 32566
    hostPort: 32566
  - containerPort: 32567
    hostPort: 32567
  - containerPort: 32568
    hostPort: 32568
  kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
        extraArgs:
          max-mutating-requests-inflight: "2000"
          max-requests-inflight: "4000"
EOF

file=./.github/workflows/chainsaw-e2e-test-${major}.yaml

	cat <<EOF > "${file}"
${HEADER}
name: chainsaw e2e test ${major}
on: [push]
concurrency:
  group: chainsaw-${major}-\${{ github.ref }}-1
  cancel-in-progress: true
permissions:
  contents: read
jobs:
  test:
    name: chainsaw e2e test ${major}
    runs-on: ubuntu-latest
    steps:
      - name: Cancel Previous Runs
        uses: styfle/cancel-workflow-action@0.9.1
        with:
          access_token: \${{ github.token }}

      - name: Checkout
        uses: actions/checkout@v4
        with:
          submodules: true

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.21'

      - name: Vendor Dependencies
        run: make vendor vendor.check

      - name: Docker cache
        uses: ScribeMD/docker-cache@0.3.7
        with:
          key: docker-\${{ runner.os }}-\${{ hashFiles('go.sum') }}}

      - name: Run chainsaw tests ${major}
        run: make chainsaw
        env:
          LATEST_KUBE_VERSION: '${major}'
          AWS_ACCESS_KEY_ID: 'Dummy'
          AWS_SECRET_ACCESS_KEY: 'Dummy'
          AWS_DEFAULT_REGION: 'us-east-1'
EOF

done
