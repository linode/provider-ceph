#!/bin/bash

: "${TEST_KIND_NODES?= required}"
: "${REPO?= required}"
: "${LOCALSTACK_VERSION?= required}"
: "${CERT_MANAGER_VERSION?= required}"

# This script reads a comma-delimited string TEST_KIND_NODES of kind node versions
# for kuttl tests to be run on, and generates the relevant files for each version.

IFS=', ' read -r -a kind_nodes <<< "$TEST_KIND_NODES"


# remove existing files
rm -f ./e2e/kind/*
rm -rf ./e2e/kuttl/*

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
	# write kuttl config file for version
	if [ ! -d "./e2e/kuttl/ceph" ]; then
		mkdir -p ./e2e/kuttl/ceph
    fi

    if [ ! -d "./e2e/kuttl/stable" ]; then
		mkdir -p ./e2e/kuttl/stable
	fi
	file=./e2e/kuttl/stable/${REPO}-${major}.yaml
    file_ceph=./e2e/kuttl/ceph/${REPO}-${major}.yaml

	# tests use 'stable' testDir in CI
	cat <<EOF > "${file}"
${HEADER}
apiVersion: kuttl.dev/v1beta1
kind: TestSuite
testDirs:
- ./e2e/tests/stable
kindConfig: e2e/kind/kind-config-${major}.yaml
startKIND: false
timeout: 120
EOF

	# tests use 'ceph' testDir for manual tests
	cat <<EOF > "${file_ceph}"
${HEADER}
apiVersion: kuttl.dev/v1beta1
kind: TestSuite
testDirs:
- ./e2e/tests/ceph
kindConfig: e2e/kind/kind-config-${major}.yaml
startKIND: false
timeout: 120
EOF

file=./.github/workflows/kuttl-e2e-test-${major}.yaml

	cat <<EOF > "${file}"
${HEADER}
name: kuttl e2e test ${major}
on: [push]
concurrency:
  group: kuttl-${major}-\${{ github.ref }}-1
  cancel-in-progress: true
permissions:
  contents: read
jobs:
  test:
    name: kuttl e2e test ${major}
    runs-on: ubuntu-latest
    env:
      KUTTL: /usr/local/bin/kubectl-kuttl
    steps:
      - name: Cancel Previous Runs
        uses: styfle/cancel-workflow-action@0.9.1
        with:
          access_token: \${{ github.token }}
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'
      - name: Install dependencies
        run: |
          sudo curl -Lo \$KUTTL https://github.com/kudobuilder/kuttl/releases/download/v0.13.0/kubectl-kuttl_0.13.0_linux_x86_64
          sudo chmod +x \$KUTTL
      - name: Build
        run: make submodules build
      - name: Run kuttl tests ${major}
        run: make kuttl
        env:
          WEBHOOK_TYPE: 'cert-manager'
          LATEST_KUBE_VERSION: '${major}'
          AWS_ACCESS_KEY_ID: 'Dummy'
          AWS_SECRET_ACCESS_KEY: 'Dummy'
          AWS_DEFAULT_REGION: 'us-east-1'
      - uses: actions/upload-artifact@v4
        if: \${{ always() }}
        with:
          name: kind-logs
          path: kind-logs-*
EOF

done
