#!/bin/bash

: "${BUILD_REGISTRY?= required}"
: "${PROJECT_NAME?= required}"
: "${ARCH?= required}"
: "${VERSION?= required}"

# Apply a configuration for the provider deployment.
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: config
spec:
  deploymentTemplate:
    spec:
      selector: {}
      template:
        spec:
          containers:
          - name: package-runtime
            image: ${BUILD_REGISTRY}/${PROJECT_NAME}-${ARCH}
            args:
            - --zap-devel
            - --kube-client-rate=80000
            - --reconcile-timeout=5s
            - --max-reconcile-rate=600
            - --reconcile-concurrency=160
            - --poll=30m
            - --sync=1h
            - --assume-role-arn=${ASSUME_ROLE_ARN}
EOF

# Apply the provider.
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: ${PROJECT_NAME}
spec:
  package: ${PROJECT_NAME}-${VERSION}.gz
  packagePullPolicy: Never
  runtimeConfigRef:
    name: config
EOF

