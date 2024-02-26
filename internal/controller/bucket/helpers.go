package bucket

import (
	"context"
	"strings"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/utils"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

// isBucketPaused returns true if the bucket has the paused label set.
func isBucketPaused(bucket *v1alpha1.Bucket) bool {
	if val, ok := bucket.Labels[meta.AnnotationKeyReconciliationPaused]; ok && val == True {
		return true
	}

	return false
}

// pauseBucket sets the bucket's pause label to true.
func pauseBucket(bucket *v1alpha1.Bucket) {
	if bucket.ObjectMeta.Labels == nil {
		bucket.ObjectMeta.Labels = map[string]string{}
	}
	bucket.Labels[meta.AnnotationKeyReconciliationPaused] = True
}

// isPauseRequired determines if the Bucket should be paused.
func isPauseRequired(b *v1alpha1.Bucket, c map[string]backendstore.S3Client, bb *bucketBackends, autopauseEnabled bool) bool {
	if !bb.isBucketAvailableOnBackends(b, c) {
		return false
	}

	// If lifecycle config is enabled and is specified in the spec, we should only pause once
	// the lifecycle config is available on all backends.
	if !b.Spec.LifecycleConfigurationDisabled && b.Spec.ForProvider.LifecycleConfiguration != nil && !bb.isLifecycleConfigAvailableOnBackends(b, c) {
		return false
	}

	// If lifecycle config is disabled, we should only pause once the lifecycle config is
	// removed from all backends.
	if b.Spec.LifecycleConfigurationDisabled && !bb.isLifecycleConfigRemovedFromBackends(b, c) {
		return false
	}

	return (b.Spec.AutoPause || autopauseEnabled) &&
		// Only return true if this label value is "".
		// This is to allow the user to delete a paused bucket with autopause enabled.
		// By setting this value to "false" or some other no-empty-string value, the
		// Update loop can bypass autopause, subsequently enabling deletion to take place.
		b.Labels[meta.AnnotationKeyReconciliationPaused] == ""
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

// setBackendLabels adds label "provider-ceph.backends.<backend-name>" to the Bucket for each backend.
func setBackendLabels(bucket *v1alpha1.Bucket, providerNames []string) {
	if bucket.ObjectMeta.Labels == nil {
		bucket.ObjectMeta.Labels = map[string]string{}
	}

	labelsToDelete := []string{}
	for k := range bucket.ObjectMeta.Labels {
		if strings.HasPrefix(k, v1alpha1.BackendLabelPrefix) && bucket.ObjectMeta.Labels[k] == True {
			labelsToDelete = append(labelsToDelete, k)
		}
	}
	for _, k := range labelsToDelete {
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

func setBucketStatus(bucket *v1alpha1.Bucket, bucketBackends *bucketBackends, providerNames []string) {
	bucket.Status.SetConditions(xpv1.Unavailable())

	backends := bucketBackends.getBackends(bucket.Name, providerNames)
	bucket.Status.AtProvider.Backends = backends

	for _, backend := range backends {
		if backend.BucketCondition.Equal(xpv1.Available()) {
			// If the bucket is Availailable on ANY backend,
			// the Bucket CR is also considered Available.
			bucket.Status.SetConditions(xpv1.Available())

			break
		}
	}
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
