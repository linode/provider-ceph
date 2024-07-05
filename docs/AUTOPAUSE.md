# Autopause

## Description
Crossplane has an existing [pause feature](https://docs.crossplane.io/latest/concepts/managed-resources/#paused) which allows the user to pause reconciliation of a managed resource by applying a specific annotation to the managed resource's metadata. Provider Ceph uses a variation of this mechanism to achieve the same result by using an identical label instead of the described annotation.

The Provider Ceph controller manager, created upon start-up, caches only Bucket CRs which have _not_ been paused by the label. As such, only non-paused Bucket CRs will be reconciled by Provider Ceph, achieveing the desired "pause" effect. 

If Autopause is enabled, Provider Ceph will automatically pause a Bucket CR once all corresponding S3 buckets are in a `Ready` state on the relevant backends and the CR is considered `Synced`.

## Enabling Autopause
 - Autopause can be enabled globally for **all Bucket CRs** by setting the appropriate Provider Ceph flag `--auto-pause-bucket=true`.
 - Autopause can also be enabled **per Bucket CR** by setting `autoPause: true` in the Bucket CR Spec. 

**Note:** The global flag takes precedence over the setting of an individual Bucket CR.

> [!WARNING]
> It is the responsibility of the user/client to "unpause" a paused Bucket CR before performing an Update or Delete operation.

## Updating a Paused Bucket
A paused Bucket CR can be updated like any other CR. However, the changes will _not_ trigger a reconciliation of the CR by Provider Ceph. To temporarily "unpause" a Bucket CR to allow Provider Ceph to reconcile an update, the Bucket CR pause label must be set to an empty string `""`. This will result in Provider Ceph reconciling an updated CR and then pausing the Bucket CR once again after the update is complete and the CR is considered `Synced`.

## Deleting a Paused Bucket
To temporarily "unpause" a Bucket CR to allow Provider Ceph to perform Delete, the Bucket CR must be patched, setting the pause label to `"false"` or some other value that is **not** an empty string `""`.
```
kubectl patch bucket --type=merge <your-bucket-name> -p '{"metadata":{"labels":{"crossplane.io/paused":"false"}}}'
```
The Bucket CR can then be deleted as normal:
```
kubectl delete bucket <your-bucket-name>
```
