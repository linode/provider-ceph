# provider-ceph

`provider-ceph` is a minimal [Crossplane](https://crossplane.io/) Provider
that reconciles `Bucket` CRs with multiple external S3 backends such as Ceph. It comes
with the following features:

- A `ProviderConfig` type that represents a single S3 backend (such as Ceph) and points to a credentials `Secret` for access to that backend.
- A controller that reconciles `ProviderConfig` objects which represent S3 backends and stores client details for each backend.
- A `Bucket` resource type that represents an S3 bucket.
- A controller that observes `Bucket` objects and reconciles these objects with the S3 backends.

## Developing

Spin up the test environment, but with `provider-ceph` running locally in your terminal:

```
make dev
```

After you've made some changes, kill (Ctrl+C) the existing `provider-ceph` and re-run it:

```
make run
```

### Debugging
Spin up the test environment, but with `provider-ceph` running locally in your terminal:

```
make mirrord.cluster mirrord.run
```

For debugging please install `mirrord` plugin in your IDE of choice.

Refer to Crossplane's [CONTRIBUTING.md] file for more information on how the
Crossplane community prefers to work. The [Provider Development][provider-dev]
guide may also be of use.

[CONTRIBUTING.md]: https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md
[provider-dev]: https://github.com/crossplane/crossplane/blob/master/docs/contributing/provider_development_guide.md

## Getting Started

[Install Crossplane](https://docs.crossplane.io/v1.11/software/install/#install-crossplane) in you Kubernetes cluster

Install the provider by using the Upbound CLI after changing the image tag to the latest release:

```
up ctp provider install xpkg.upbound.io/linode/provider-ceph:v0.1.0
```

Alternatively, you can use declarative installation:
```
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: linode-provider-ceph
spec:
  package: xpkg.upbound.io/linode/provider-ceph:v0.1.0
EOF
```
See [WEBHOOKS.md](docs/WEBHOOKS.md) for instructions on how to enable webhooks.

### Customizing provider deployment

Crossplane uses `DeploymentRuntimeConfig` object to apply customizations on the provider.
Here are a few examples:


```yaml
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
            args:
            - --kube-client-rate=80000
            - --reconcile-timeout=5s
            - --max-reconcile-rate=600
            - --reconcile-concurrency=160
            - --poll=30m
            - --sync=1h
            - --assume-role-arn=[ASSUME_ROLE_ARN]
```

You have to attach `DeploymentRuntimeConfig` to the `Provider` object.

```yaml
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-ceph
spec:
  runtimeConfigRef:
    name: provider-ceph
```

### Enabling validation webhook

Validation webhook is not registered on the Kubernetes cluster by default. But before you enable it,
you have to deploy cert manager.

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml
```

You have to create Issuer and Certificate.

```yaml
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
```

You need a `DeploymentRuntimeConfig` too ([Customizing provider deployment](#customizing-provider-deployment)).

```yaml
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
            args:
            - --webhook-tls-cert-dir=/certs
            volumeMounts:
            - name: cert-manager-certs
              mountPath: /certs
          volumes:
          - name: cert-manager-certs
            secret:
              secretName: crossplane-provider-provider-ceph-server-cert
```

Finaly, you have to apply `ValidatingWebhookConfiguration`.

```bash
PROV_CEPH_VER=v0.0.32 kubectl apply -f https://github.com/linode/provider-ceph/blob/release-${PROV_CEPH_VER}/package/webhookconfigurations/manifests.yaml
```

## Contact
- Slack: Join our [#provider-ceph](https://crossplane.slack.com/archives/C05RKQRNDHA) slack channel.
