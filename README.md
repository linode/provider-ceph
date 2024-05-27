# provider-ceph

`provider-ceph` is a minimal [Crossplane](https://crossplane.io/) Provider
that reconciles `Bucket` CRs with multiple external S3 backends such as Ceph. It comes
with the following features:

- A `ProviderConfig` type that represents a single S3 backend (such as Ceph) and points to a credentials `Secret` for access to that backend.
- A controller that reconciles `ProviderConfig` objects which represent S3 backends and stores client details for each backend.
- A `Bucket` resource type that represents an S3 bucket.
- A controller that observes `Bucket` objects and reconciles these objects with the S3 backends.

## Getting Started

[Install Crossplane](https://docs.crossplane.io/v1.11/software/install/#install-crossplane) in you Kubernetes cluster

Install the provider by using the Upbound CLI after changing the image tag to the latest release:

```
up ctp provider install xpkg.upbound.io/linode/provider-ceph:v0.0.46-rc.0.5.gbd51690
```

Alternatively, you can use declarative installation:
```
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: linode-provider-ceph
spec:
  package: xpkg.upbound.io/linode/provider-ceph:v0.0.46-rc.0.5.gbd51690
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

## Contact
- Slack: Join our [#provider-ceph](https://crossplane.slack.com/archives/C05RKQRNDHA) slack channel.

## Development

Refer to Crossplane's [CONTRIBUTING.md](https://github.com/crossplane/crossplane/tree/master/contributing) file for more information on how the
Crossplane community prefers to work. The [guide-provider-development.md](https://github.com/crossplane/crossplane/blob/master/contributing/guide-provider-development.md)
guide may also be of use. For more information about how to setup your development environment, please follow our [DEVELOPMENT.md](docs/DEVELOPMENT.md) page.
