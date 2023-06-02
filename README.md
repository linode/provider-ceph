# provider-ceph

`provider-ceph` is a minimal [Crossplane](https://crossplane.io/) Provider
that reconciles `Bucket` CRs with multiple external S3 backends such as Ceph. It comes
with the following features:

- A `ProviderConfig` type that represents a single S3 backend (such as Ceph) and points to a credentials `Secret` for access to that backend.
- A controller that reconciles `ProviderConfig` objects which represent S3 backends and stores client details for each backend.
- A `Bucket` resource type that represents an S3 bucket.
- A controller that observes `Bucket` objects and reconciles these objects with the S3 backends.

## Testing

The E2E test setup consists of the following:
- A single [Kind](https://kind.sigs.k8s.io/) cluster with [Crossplane](https://www.crossplane.io/) installed and `provider-ceph` deployed.
- Three [LocalStack](https://localstack.cloud/) instances representing the S3 backends. These are created using Docker Compose.

The tests are run using [Kuttl](https://kuttl.dev/) and s3 backend operations are verified using the [AWS CLI](https://aws.amazon.com/cli/).

![provider-ceph-testing drawio](https://user-images.githubusercontent.com/41484746/236199553-06990687-462a-4097-8d42-a7f7f055abbf.png)

### Run Kuttl Test Suite

```
make kuttl
```

## Developing
Spin up the test environment, but with `provider-ceph` running locally in your terminal:

```
make dev
```

After you've made some changes, kill (Ctrl+C) the existing `provider-ceph` and re-run it:

```
make run
```

Refer to Crossplane's [CONTRIBUTING.md] file for more information on how the
Crossplane community prefers to work. The [Provider Development][provider-dev]
guide may also be of use.

[CONTRIBUTING.md]: https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md
[provider-dev]: https://github.com/crossplane/crossplane/blob/master/docs/contributing/provider_development_guide.md

## Getting Started

[Install Crossplane](https://docs.crossplane.io/v1.11/software/install/#install-crossplane) in you Kubernetes cluster

Install the provider by using the Upbound CLI after changing the image tag to the latest release:

```
up ctp provider install linode/provider-ceph:v0.0.3
```

Alternatively, you can use declarative installation:
```
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: linode-provider-ceph
spec:
  package: linode/provider-ceph:v0.0.3
EOF
```
