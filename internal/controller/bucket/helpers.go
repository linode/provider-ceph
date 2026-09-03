package bucket

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/utils"
	"go.opentelemetry.io/otel"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

const errUnavailableBackends = "Bucket is unavailable on the following backends: %s"

const errNoResourceVersion = "cannot patch Bucket CR with an optimistic lock: the read returned no resourceVersion"

const (
	patchRetrySteps    = 5
	patchRetryDuration = 10 * time.Millisecond
	patchRetryFactor   = 2.0
	patchRetryJitter   = 0.1
)

// patchBackoff is the conflict budget for updateBucketCR. Both of its patches carry an
// optimistic lock, so conflicts with the health-check controller and with external
// clients are expected and have to be absorbed here.
var patchBackoff = wait.Backoff{
	Steps:    patchRetrySteps,
	Duration: patchRetryDuration,
	Factor:   patchRetryFactor,
	Jitter:   patchRetryJitter,
}

// isBucketPaused returns true if the bucket has the paused label set.
func isBucketPaused(bucket *v1alpha1.Bucket) bool {
	if val, ok := bucket.Labels[meta.AnnotationKeyReconciliationPaused]; ok && val == consts.TrueStr {
		return true
	}

	return false
}

// pauseAllowed returns false when the Bucket CR must stay in the controller's
// cache. A paused Bucket CR is filtered out of that cache, so pausing one that
// is disabled or deleting means the loops that tear it down never run again.
func pauseAllowed(bucket *v1alpha1.Bucket) bool {
	return !bucket.Spec.Disabled && bucket.GetDeletionTimestamp().IsZero()
}

// isPauseRequired determines if the Bucket should be paused.
//
//nolint:gocyclo,cyclop // Function requires numerous checks.
func isPauseRequired(bucket *v1alpha1.Bucket, providerNames []string, c map[string]backendstore.S3Client, bb *bucketBackends, autopauseEnabled bool) bool {
	// Avoid pausing a Bucket CR that is being torn down. A disabled Bucket CR has
	// its buckets removed from the backends by the Update loop, and a deleting one
	// has them removed by the Delete loop.
	if !pauseAllowed(bucket) {
		return false
	}

	// Avoid pausing if the Bucket CR is not Ready or not Synced.
	if !bucket.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) ||
		!bucket.Status.GetCondition(xpv1.TypeSynced).Equal(xpv1.ReconcileSuccess()) {
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

	// If SSE config is enabled and is specified in the spec, we should only pause once
	// the SSE config is available on all backends.
	if !bucket.Spec.ServerSideEncryptionConfigurationDisabled &&
		bucket.Spec.ForProvider.ServerSideEncryptionConfiguration != nil &&
		!bb.isSSEConfigAvailableOnBackends(bucket, providerNames, c) {
		return false
	}

	// If SSE config is disabled, we should only pause once the SSE config is
	// removed from all backends.
	if bucket.Spec.ServerSideEncryptionConfigurationDisabled && !bb.isSSEConfigRemovedFromBackends(bucket, providerNames, c) {
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

	// Avoid pausing when an object lock configuration is specified in the spec, but not all
	// object lock configs are available.
	if bucket.Spec.ForProvider.ObjectLockConfiguration != nil && !bb.isObjectLockConfigAvailableOnBackends(bucket.Name, providerNames, c) {
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
	for k, v := range bucket.Labels {
		if !enabledOnly || strings.HasPrefix(k, v1alpha1.BackendLabelPrefix) && bucket.Labels[k] == consts.TrueStr {
			backends[strings.Replace(k, v1alpha1.BackendLabelPrefix, "", 1)] = v
		}
	}

	return backends
}

// setAllBackendLabels adds label "provider-ceph.backends.<backend-name>" to the Bucket for each backend.
func setAllBackendLabels(bucket *v1alpha1.Bucket, providerNames []string) {
	if bucket.Labels == nil {
		bucket.Labels = map[string]string{}
	}

	// Delete existing labels except explicitly disabled backend labels.
	for k := range getAllBackendLabels(bucket, true) {
		delete(bucket.Labels, k)
	}

	for _, beName := range providerNames {
		beLabel := utils.GetBackendLabel(beName)
		if _, ok := bucket.Labels[beLabel]; ok {
			continue
		}

		bucket.Labels[beLabel] = consts.TrueStr
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
		// Skip explicitly disabled backends
		beLabel := utils.GetBackendLabel(providers[i])
		if status, ok := bucket.Labels[beLabel]; ok && status != consts.TrueStr {
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

	var ok uint = 0
	unavailableBackends := make([]string, 0)
	for backendName, backend := range backends {
		if backend.BucketCondition.Equal(xpv1.Available()) {
			ok++

			continue
		}
		unavailableBackends = append(unavailableBackends, backendName)
	}
	// The Bucket CR is considered Available if the bucket is available on "minReplicas"
	// number of backends (default = 1).
	if ok >= minReplicas {
		bucket.Status.SetConditions(xpv1.Available())
	}
	// The Bucket CR is considered Synced (ReconcileSuccess) once the bucket is available
	// on all backends. We also ensure that the overall Bucket CR is available (in a Ready
	// state) - this should already be the case.
	if ok >= uint(len(providerNames)) &&
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

// updateBucketCR applies a series of callbacks to the latest version of the Bucket CR
// and patches the result.
//
// Callbacks return an UpdateRequired status, depending on whether the callback changed
// the Bucket CR Status (NeedsStatusUpdate) or the Bucket CR object (NeedsObjectUpdate).
// This tells updateBucketCR whether to patch the status subresource or the object.
//
// Both patches carry an optimistic lock, so a write made by another client between the
// read and the patch is rejected with a conflict instead of being silently overwritten,
// and the conflict is retried against a fresh read from the API. A callback must
// therefore be safe to run more than once, and must base its decisions only on the
// Bucket CR it is handed, because that object changes between attempts.
//
// Callback example, updating the latest version of bucket Status with a string, so
// NeedsStatusUpdate is returned to have updateBucketCR patch the status subresource.
//
//	func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
//	  bucketLatest.Status.SomeOtherField = "some-value"
//
//	  return NeedsStatusUpdate
//	},
//
// Example usage with above callback example:
//
//	err := updateBucketCR(ctx, bucket, func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
//	  bucketLatest.Status.SomeOtherField = "some-value"
//
//	  return NeedsStatusUpdate
//	})
//
//	if err != nil {
//	  // Handle error
//	}
func (c *external) updateBucketCR(ctx context.Context, bucket *v1alpha1.Bucket, callbacks ...func(*v1alpha1.Bucket) UpdateRequired) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.external.updateBucketCR")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, c.log)

	for i, cb := range callbacks {
		firstAttempt := true

		err := retry.RetryOnConflict(patchBackoff, func() error {
			// Only the first read of the first callback comes from the client cache.
			// Every later read goes straight to the API, because a Patch has just landed
			// and the cache lags it, and because a retry means the object moved on since
			// the last read. Reading everything from the API would multiply the request
			// count on a rate limited rest config, and the optimistic lock below already
			// turns a stale read into a conflict instead of a silent overwrite.
			reader := c.kubeReader
			if i == 0 && firstAttempt {
				reader = c.kubeClient
			}

			firstAttempt = false

			if err := reader.Get(
				ctx,
				types.NamespacedName{Name: bucket.GetName()},
				bucket,
			); err != nil {
				return err
			}

			// MergeFromWithOptimisticLock turns an empty resourceVersion into an opaque
			// error that is neither a conflict nor a NotFound, so it would be neither
			// retried nor recognised below. A reader that returns an object without one
			// is a bug, so name it.
			if bucket.GetResourceVersion() == "" {
				return errors.New(errNoResourceVersion)
			}

			bucketCopy := bucket.DeepCopy()
			switch cb(bucket) {
			case NeedsStatusUpdate:
				// status.atProvider.backends is only ever written here, and a merge patch
				// built from a stale read omits the backends key entirely, which leaves
				// the old value on the server. So this patch needs the lock just as much
				// as the object patch does.
				return c.kubeClient.Status().Patch(ctx, bucket,
					client.MergeFromWithOptions(bucketCopy, client.MergeFromWithOptimisticLock{}))
			case NeedsObjectUpdate:
				// Patch with an optimistic lock so a write made by another client
				// between the Get above and this Patch is rejected with a conflict and
				// retried, rather than silently overwritten. A plain merge patch
				// carries no resourceVersion precondition, so it always wins over
				// the health-check controller and any external client that writes
				// the Bucket CR with a full Update.
				return c.kubeClient.Patch(ctx, bucket,
					client.MergeFromWithOptions(bucketCopy, client.MergeFromWithOptimisticLock{}))
			default:
				return nil
			}
		})

		if err != nil {
			if kerrors.IsNotFound(err) {
				log.Info("Bucket doesn't exists", consts.KeyBucketName, bucket.Name)

				break
			}

			return errors.Wrap(err, "unable to update object")
		}
	}

	return nil
}
