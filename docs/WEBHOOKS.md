# Webhooks

## Enable Webhooks
- Webhooks are enabled in Crossplane by default from `v1.13` onwards. For previous versions of Crossplane, include the flag `--set webhooks.enabled=true` when [installing Crossplane via Helm](https://docs.crossplane.io/v1.11/software/install/#install-the-crossplane-helm-chart).
- To enable webhooks in Provider Ceph, set the `--enable-webhooks` flag for the Provider Ceph controller. See example below using a controller configuration:

`Provider` with reference to a `ControllerConfig` (**Note:** package version is omitted):
```
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-ceph
spec:
  package: xpkg.upbound.io/linode/provider-ceph:vX.X.X
  controllerConfigRef:
    name: provider-ceph
```
`ControllerConfig` with arguments:
```
apiVersion: pkg.crossplane.io/v1alpha1
kind: ControllerConfig
metadata:
  name: provider-ceph
spec:
  args:
  - "--enable-webhooks"
```
**Note:** `ControllerConfig` has been deprecated, but remains in use until an alternative exists.

## Bucket Admission Controlling Webhook
Provider Ceph provides Dynamic Admission Control for Buckets.
Create and Update operations on Buckets are blocked by the bucket admission webhook when:
- The Bucket contains one or more providers (`bucket.spec.Providers`) that do not exist (i.e. a `ProviderConfig` of the same name does not exist in the k8s cluster).

Future Work (not yet implemented):
- Bucket Lifecycle Configurations cannot be validated against a backend.
