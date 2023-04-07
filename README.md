# provider-ceph

`provider-ceph` is a minimal [Crossplane](https://crossplane.io/) Provider
that is meant to be used as a ceph for implementing new Providers. It comes
with the following features that are meant to be refactored:

- A `ProviderConfig` type that only points to a credentials `Secret`.
- A `MyType` resource type that serves as an example managed resource.
- A managed resource controller that reconciles `MyType` objects and simply
  prints their configuration in its `Observe` method.

## Developing

1. Use this repository as a ceph to create a new one.
1. Run `make submodules` to initialize the "build" Make submodule we use for CI/CD.
1. Rename the provider by running the follwing command:
```
  make provider.prepare provider={PascalProviderName}
```
4. Add your new type by running the following command:
```
make provider.addtype provider={PascalProviderName} group={group} kind={type}
```
5. Replace the *sample* group with your new group in apis/{provider}.go
5. Replace the *mytype* type with your new type in internal/controller/{provider}.go
5. Replace the default controller and ProviderConfig implementations with your own
5. Run `make reviewable` to run code generation, linters, and tests.
5. Run `make build` to build the provider.

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
- Run provider locally for debugging
```
make run
```
- In a separate terminal, edit examples/provider/config.yaml
  - Edit Secret: Add `access_key and `secret_key` from `s3_admin.cfg` (config file for you ceph cluster)
  - Edit ProviderConfig: add `host_base` from `s3_admin.cfg`

- Create Secret and ProviderConfig
```
kubectl create ns crossplane-system 

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
