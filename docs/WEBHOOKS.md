# Webhooks

## Enable Webhooks
- Webhooks are enabled in Crossplane by default from `v1.13` onwards. For previous versions of Crossplane, include the flag `--set webhooks.enabled=true` when [installing Crossplane via Helm](https://docs.crossplane.io/v1.11/software/install/#install-the-crossplane-helm-chart).

## Bucket Admission Controlling Webhook
Provider Ceph provides Dynamic Admission Control for Buckets.

### Bucket Validation Webhook
Create and Update operations on Buckets are blocked by the bucket admission webhook when:
- The Bucket contains one or more providers (`bucket.spec.Providers`) that do not exist (i.e. a `ProviderConfig` of the same name does not exist in the k8s cluster).

### Bucket Defaulter Webhook
During a Bucket Update, the defaulter webhook replicates the Bucket's pause annotation as a label and adds it to to the Bucket metadata.
This enables Provider Ceph to cache Buckets objects by label (i.e. only caching Bucket objects which haven't been paused). This webhook will block the Update on any failed case.

### Bucket Lifecycle Configurations
Future Work (not yet implemented):
- Bucket Lifecycle Configurations cannot be validated against a backend.
