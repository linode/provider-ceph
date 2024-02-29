#!/bin/bash

: "${BUILD_REGISTRY?= required}"
: "${PROJECT_NAME?= required}"
: "${ARCH?= required}"
: "${VERSION?= required}"

# Apply cert-manager Issuer and Certificate
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: selfsigned-issuer
  namespace: crossplane-system
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: crossplane-provider-provider-ceph
  namespace: crossplane-system
spec:
  commonName: provider-ceph.crossplane-system.svc
  dnsNames:
  - provider-ceph.crossplane-system.svc.cluster.local
  - provider-ceph.crossplane-system.svc
  - crossplane-provider-provider-ceph.crossplane-system.svc.cluster.local
  - crossplane-provider-provider-ceph.crossplane-system.svc
  issuerRef:
    kind: Issuer
    name: selfsigned-issuer
  secretName: crossplane-provider-provider-ceph-server-cert
EOF

# Apply a configuration for the provider deployment.
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: provider-ceph
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
            - --webhook-tls-cert-dir=/certs
            volumeMounts:
            - name: cert-manager-certs
              mountPath: /certs
          volumes:
          - name: cert-manager-certs
            secret:
              secretName: crossplane-provider-provider-ceph-server-cert
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
    name: provider-ceph
EOF

