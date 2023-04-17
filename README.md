# provider-ceph

`provider-ceph` is a minimal [Crossplane](https://crossplane.io/) Provider
that reconciles `Bucket` CRs with an external Ceph cluster. It comes
with the following features:

- A `ProviderConfig` type that points to a credentials `Secret` for access to a Ceph cluster.
- A `Bucket` resource type that serves as an example managed resource.
- A managed resource controller that reconciles `Bucket` objects and reconciles these objects with the Ceph cluster.

## Developing

1. Run `make submodules` to initialize the "build" Make submodule we use for CI/CD.
2. Add your new type by running the following command:
```
make provider.addtype provider={Ceph} group={group} kind={type}
```
2. Replace the *sample* group with your new group in apis/{provider}.go
2. Replace the *mytype* type with your new type in internal/controller/{provider}.go
2. Replace the default controller and ProviderConfig implementations with your own
2. Run `make reviewable` to run code generation, linters, and tests.
2. Run `make build` to build the provider.

Refer to Crossplane's [CONTRIBUTING.md] file for more information on how the
Crossplane community prefers to work. The [Provider Development][provider-dev]
guide may also be of use.

[CONTRIBUTING.md]: https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md
[provider-dev]: https://github.com/crossplane/crossplane/blob/master/docs/contributing/provider_development_guide.md

## Demo

### Prerequisites
- Access to a running ceph cluster

### Steps
- Create a local k8s cluster
```
kind create cluster
```
- Install necessary CRDs
```
 kubectl apply -f package/crds
```
- Initialize the build
```
make submodules
```
- Run provider locally for debugging
```
make run
```
- In a separate terminal, edit examples/provider/config.yaml
  - Edit Secret: Add `access_key and `secret_key` from `s3_admin.cfg` (config file for you ceph cluster)
  - Edit ProviderConfig: add `host_base` from `s3_admin.cfg`

- Create Secret and ProviderConfig
```
kubectl apply -f examples/provider/config.yaml
```
- Check ceph cluster for existing buckets
```
s3cmd --config /path/to/s3_admin.cfg ls
```
- Create Bucket CR 
```
kubectl apply -f examples/sample/bucket.yaml
```
- Check ceph cluster for newly created bucket
```
s3cmd --config /path/to/s3_admin.cfg ls
```
- Delete Bucket CR 
```
kubectl delete -f examples/sample/bucket.yaml
```
- Check ceph cluster to verify bucket was deleted
```
s3cmd --config /path/to/s3_admin.cfg ls
```
