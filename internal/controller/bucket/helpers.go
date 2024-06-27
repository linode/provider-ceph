package bucket

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/utils"
	"go.opentelemetry.io/otel"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

const errUnavailableBackends = "Bucket is unavailable on the following backends: %s"

// isBucketPaused returns true if the bucket has the paused label set.
func isBucketPaused(bucket *v1alpha1.Bucket) bool {
	if val, ok := bucket.Labels[meta.AnnotationKeyReconciliationPaused]; ok && val == True {
		return true
	}

	return false
}

// isPauseRequired determines if the Bucket should be paused.
//
//nolint:gocyclo,cyclop // Function requires numerous checks.
func isPauseRequired(bucket *v1alpha1.Bucket, providerNames []string, c map[string]backendstore.S3Client, bb *bucketBackends, autopauseEnabled bool) bool {
	// Avoid pausing if the Bucket CR is not Ready and Synced.
	if !(bucket.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) &&
		bucket.Status.GetCondition(xpv1.TypeSynced).Equal(xpv1.ReconcileSuccess())) {
		return false
	}

	// Avoid pausing if the number of backends on which the bucket is available is less than the number of providerNames.
	if float64(bb.countBucketsAvailableOnBackends(bucket.Name, providerNames, c)) < float64(len(providerNames)) {
		return false
	}

	// If lifecycle config is enabled and is specified in the spec, we should only pause once
	// the lifecycle config is available on all backends.
	if !bucket.Spec.LifecycleConfigurationDisabled && bucket.Spec.ForProvider.LifecycleConfiguration != nil && !bb.isLifecycleConfigAvailableOnBackends(bucket, providerNames, c) {
		return false
	}

	// If lifecycle config is disabled, we should only pause once the lifecycle config is
	// removed from all backends.
	if bucket.Spec.LifecycleConfigurationDisabled && !bb.isLifecycleConfigRemovedFromBackends(bucket, providerNames, c) {
		return false
	}

	// Avoid pausing when a versioning configuration is specified in the spec, but not all
	// versioning configs are available.
	if bucket.Spec.ForProvider.VersioningConfiguration != nil && !bb.isVersioningConfigAvailableOnBackends(bucket.Name, providerNames, c) {
		return false
	}

	// Avoid pausing when versioning configurations exist on backends, but not all
	// versioning configs are available. This scenario can occur when the versioning
	// config has been removed from the Spec (and is therefore suspended).
	if !bb.isVersioningConfigRemovedFromBackends(bucket.Name, providerNames, c) && !bb.isVersioningConfigAvailableOnBackends(bucket.Name, providerNames, c) {
		return false
	}

	return (bucket.Spec.AutoPause || autopauseEnabled) &&
		// Only return true if this label value is "".
		// This is to allow the user to delete a paused bucket with autopause enabled.
		// By setting this value to "false" or some other no-empty-string value, the
		// Update loop can bypass autopause, subsequently enabling deletion to take place.
		bucket.Labels[meta.AnnotationKeyReconciliationPaused] == ""
}

// isBucketAvailableFromStatus checks the backends listed in providerNames against the
// backends in Status to ensure buckets are considered Available on all desired backends.
func isBucketAvailableFromStatus(bucket *v1alpha1.Bucket, providerNames []string, backendClients map[string]backendstore.S3Client) bool {
	for _, backendName := range providerNames {
		if _, ok := backendClients[backendName]; !ok {
			// This backend does not exist in the list of available backends.
			// The backend may be offline, so it is skipped.
			continue
		}

		if backend := bucket.Status.AtProvider.Backends[backendName]; backend == nil {
			// The bucket has not been created on this backend.
			return false
		} else if !backend.BucketCondition.Equal(xpv1.Available()) {
			// The bucket is not Available on this backend.
			return false
		}
	}

	return true
}

// getAllBackendLabels returns all "provider-ceph.backends.<backend-name>" labels.
func getAllBackendLabels(bucket *v1alpha1.Bucket, enabledOnly bool) map[string]string {
	backends := map[string]string{}
	for k, v := range bucket.ObjectMeta.Labels {
		if !enabledOnly || strings.HasPrefix(k, v1alpha1.BackendLabelPrefix) && bucket.ObjectMeta.Labels[k] == True {
			backends[strings.Replace(k, v1alpha1.BackendLabelPrefix, "", 1)] = v
		}
	}

	return backends
}

// setAllBackendLabels adds label "provider-ceph.backends.<backend-name>" to the Bucket for each backend.
func setAllBackendLabels(bucket *v1alpha1.Bucket, providerNames []string) {
	if bucket.ObjectMeta.Labels == nil {
		bucket.ObjectMeta.Labels = map[string]string{}
	}

	// Delete existing labels except explicitly disabled backend labels.
	for k := range getAllBackendLabels(bucket, true) {
		delete(bucket.ObjectMeta.Labels, k)
	}

	for _, beName := range providerNames {
		beLabel := utils.GetBackendLabel(beName)
		if _, ok := bucket.ObjectMeta.Labels[beLabel]; ok {
			continue
		}

		bucket.ObjectMeta.Labels[beLabel] = True
	}
}

// getBucketProvidersFilterDisabledLabel returns the specified providers or default providers,
// and filters out providers disabled by label.
func getBucketProvidersFilterDisabledLabel(bucket *v1alpha1.Bucket, providerNames []string) []string {
	providers := bucket.Spec.Providers
	if len(providers) == 0 {
		providers = providerNames
	}

	okProviders := []string{}
	for i := range providers {
		// Skip explicitly disableds
		beLabel := utils.GetBackendLabel(providers[i])
		if status, ok := bucket.Labels[beLabel]; ok && status != True {
			continue
		}

		okProviders = append(okProviders, providers[i])
	}

	return okProviders
}

// setBucketStatus sets the Bucket CR Status to Available if a bucket is Available on all providers in providerNames
// or if the minReplicas quota has been reached. Otherwise, the Bucket CR Status is set as Unavailable.
func setBucketStatus(bucket *v1alpha1.Bucket, bucketBackends *bucketBackends, providerNames []string, minReplicas uint) {
	bucket.Status.SetConditions(xpv1.Unavailable())

	backends := bucketBackends.getBackends(bucket.Name, providerNames)
	bucket.Status.AtProvider.Backends = backends

	ok := 0
	unavailableBackends := make([]string, 0)
	for backendName, backend := range backends {
		if backend.BucketCondition.Equal(xpv1.Available()) {
			ok++

			continue
		}
		unavailableBackends = append(unavailableBackends, backendName)
	}
	// The Bucket CR is considered Available if the bucket is available on any backend.
	if ok > 0 {
		bucket.Status.SetConditions(xpv1.Available())
	}
	// The Bucket CR is considered Synced (ReconcileSuccess) once the bucket is available
	// on the lesser of all backends or minimum replicas. We also ensure that the overall
	// Bucket CR is available (in a Ready state) - this should already be the case.
	if float64(ok) >= math.Min(float64(len(providerNames)), float64(minReplicas)) &&
		bucket.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) {
		bucket.Status.SetConditions(xpv1.ReconcileSuccess())

		return
	}
	// The Bucket CR cannot be considered Synced.
	slices.Sort(unavailableBackends)
	err := errors.New(fmt.Sprintf(errUnavailableBackends, strings.Join(unavailableBackends, ", ")))
	bucket.Status.SetConditions(xpv1.ReconcileError(err))
}

type UpdateRequired int

const (
	NeedsStatusUpdate UpdateRequired = iota
	NeedsObjectUpdate
)

// updateBucketCR updates the Bucket CR and/or the Bucket CR Status by applying a series of callbacks.
// The function uses an exponential backoff retry mechanism to handle potential conflicts during updates.
//
// The callbacks take two Bucket parameters. Before the callbacks are called, the first Bucket
// parameter will become a DeepCopy of bucket. The second will become the latest version of bucket, as it is fetched
// from the Kube API. Each callback function should aim to update the latest version of the bucket (second parameter)
// with the changes which will be persisted in bucket (and as a result, it's DeepCopy).
//
// Callbacks return an UpdateRequired status, depending on whether the update that is performed by the callback
// requires a Bucket Status update (NeedsStatusUpdate) or a full Bucket object update (NeedsObjectUpdate).
// This enables updateObject to make a decision on whether to perform kubeclient.Status().Update() or
// kubeClient.Update() respectively.
//
// Callback example 1, updating the latest version of bucket Status with a field from your version of bucket.
// This callback only performs an update to the Bucket Status, so NeedsStatusUpdate is returned to enabled
// updateBucketCR to perform kubeClient.Status().Update().
//
//	 func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
//		  bucketLatest.Status.SomeField = bucketDeepCopy.Status.SomeField
//
//	   return NeedsStatusUpdate
//	 },
//
// Callback example 2, updating the latest version of bucket Status with a string:
//
//		func(_, bucketLatest *v1alpha1.Bucket) {
//		  bucketLatest.Status.SomeOtherField = "some-value"
//
//	   return NeedsStatusUpdate
//		},
//
// Callback example 3, updating the latest version of bucket Spec with a field from your version of the bucket.
// This callback performs an update to the Bucket Spec, so NeedsObjectUpdate is returned to enabled updateBucketCR
// to perform a full kubeClient.Update().
//
//	 func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
//		  bucketLatest.Spec.SomeField = bucketDeepCopy.Spec.SomeField
//
//	   return NeedsObjectUpdate
//	 },
//
// Example usage with above callback example 3:
//
//		err := updateBucketCR(ctx, bucket, func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) {
//		  bucketLatest.Spec.SomeField = bucketDeepCopy.Spec.SomeField
//
//	   return NeedsObjectUpdate
//		})
//
//		if err != nil {
//		  // Handle error
//		}
func (c *external) updateBucketCR(ctx context.Context, bucket *v1alpha1.Bucket, callbacks ...func(*v1alpha1.Bucket, *v1alpha1.Bucket) UpdateRequired) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.external.updateBucketCR")
	defer span.End()

	bucketDeepCopy := bucket.DeepCopy()

	nn := types.NamespacedName{Name: bucket.GetName()}

	for _, cb := range callbacks {
		err := retry.OnError(retry.DefaultRetry, resource.IsAPIError, func() error {
			if err := c.kubeClient.Get(ctx, nn, bucket); err != nil {
				return err
			}

			switch cb(bucketDeepCopy, bucket) {
			case NeedsStatusUpdate:
				return c.kubeClient.Status().Update(ctx, bucket)
			case NeedsObjectUpdate:
				return c.kubeClient.Update(ctx, bucket)
			default:
				return nil
			}
		})

		if err != nil {
			if kerrors.IsNotFound(err) {
				c.log.Info("Bucket doesn't exists", consts.KeyBucketName, bucket.Name)

				break
			}

			return errors.Wrap(err, "unable to update object")
		}
	}

	return nil
}
