# Autopause

## Description
Autopause leverages the existing [Crossplane pause feature](https://docs.crossplane.io/latest/concepts/managed-resources/#paused) which allows the user to pause reconciliation of a managed resource by applying a specific annotation to the managed resource's metadata. If Autopause is enabled, Provider Ceph will automatically pause a Bucket CR once all corresponding S3 buckets are in a `Ready` state on the relevant backends. It is also important to note that Provider Ceph, unlike most Crossplane Providers, uses labels instead of annotations to achieve this outcome.

## Enabling Autopause
 - Autopause can be enabled globally for **all Bucket CRs** by setting the appropriate Provider Ceph flag `--auto-pause-bucket=true`.
 - Autopause can also be enabled **per Bucket CR** by setting `autoPause: true` in the Bucket CR Spec. 

**Note:** The global flag takes precedence over the setting of an individual Bucket CR.

## Deleting a Paused Bucket
To temporarily "unpause" a Bucket CR to allow Provider Ceph to perform Delete, the Bucket CR must be patched, setting the pause label to `"false"` or some other value that is **not** an empty string `""`.
```
kubectl patch bucket --type=merge <your-bucket-name> -p '{"metadata":{"labels":{"crossplane.io/paused":"false"}}}'
```
The Bucket CR can then be deleted as normal:
```
kubectl delete bucket <your-bucket-name>
```
