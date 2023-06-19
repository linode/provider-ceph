#!/bin/bash

: "${AWS_ACCESS_KEY_ID?= required}"
: "${AWS_SECRET_ACCESS_KEY?= required}"
: "${CEPH_ADDRESS?= required}"

encoded_access_key=$(echo -n ${AWS_ACCESS_KEY_ID} | base64)
encoded_secret_key=$(echo -n ${AWS_SECRET_ACCESS_KEY} | base64)

kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  namespace: crossplane-system
  name: ceph-secret
type: Opaque
data:
  access_key: "${encoded_access_key}"
  secret_key: "${encoded_secret_key}"
---
apiVersion: ceph.crossplane.io/v1alpha1
kind: ProviderConfig
metadata:
  name: ceph-cluster
  namespace: crossplane-system
spec:
  hostBase: "${CEPH_ADDRESS}"
  credentials:
    source: Secret
    secretRef:
      namespace: crossplane-system
      name: ceph-secret
      key: credentials
EOF
