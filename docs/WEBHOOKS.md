# Webhooks

## Enable Webhooks
- Webhooks are enabled in Crossplane by default from `v1.13` onwards. For previous versions of Crossplane, include the flag `--set webhooks.enabled=true` when [installing Crossplane via Helm](https://docs.crossplane.io/v1.11/software/install/#install-the-crossplane-helm-chart).

## Bucket Admission Controlling Webhook
Provider Ceph provides Dynamic Admission Control for Buckets.

### Bucket Validation Webhook
Validates Bucket CRs for Create and Update operations.
This webhook is also configured with an `objectSelector` label `provider-ceph.crossplane.io/validation-required: true`.
It is the responsibility of the user (or the external system) to ensure that incoming Bucket CRs are given this label to enable webhook validation, should validation for the CR be desired.

Create and Update operations on Buckets are blocked by the bucket admission webhook when:
- The Bucket contains one or more providers (`bucket.spec.Providers`) that do not exist (i.e. a `ProviderConfig` of the same name does not exist in the k8s cluster).
- Bucket Lifecycle Configurations cannot be validated against a backend.
