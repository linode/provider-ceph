package bucket

import (
	"context"
	"fmt"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

// isAlreadyExists helper function to test for ErrCodeBucketAlreadyOwnedByYou error
func isAlreadyExists(err error) bool {
	var alreadyOwnedByYou *s3types.BucketAlreadyOwnedByYou

	return errors.As(err, &alreadyOwnedByYou)
}

func setBucketStatus(bucket *v1alpha1.Bucket, bucketBackends *bucketBackends) {
	bucket.Status.SetConditions(xpv1.Unavailable())

	bucketBackendStatuses := bucketBackends.getBucketBackendStatuses(bucket.Name, bucket.Spec.Providers)
	bucket.Status.AtProvider.BackendStatuses = bucketBackendStatuses

	for _, backendStatus := range bucketBackendStatuses {
		if backendStatus == v1alpha1.BackendReadyStatus {
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

// Callbacks have two parameters, first bucket is the original, the second is the new version og bucket.
func (c *external) updateObject(ctx context.Context, bucket *v1alpha1.Bucket, callbacks ...func(*v1alpha1.Bucket, *v1alpha1.Bucket) UpdateRequired) error {
	origBucket := bucket.DeepCopy()

	nn := types.NamespacedName{Name: bucket.GetName()}

	const (
		steps  = 4
		divide = 2
		factor = 0.5
		jitter = 0.1
	)

	for _, cb := range callbacks {
		err := retry.OnError(wait.Backoff{
			Steps:    steps,
			Duration: c.operationTimeout / divide,
			Factor:   factor,
			Jitter:   jitter,
		}, resource.IsAPIError, func() error {
			if err := c.kubeClient.Get(ctx, nn, bucket); err != nil {
				return err
			}

			switch cb(origBucket, bucket) {
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
				c.log.Info("Bucket doesn't exists", "bucket_name", bucket.Name)

				break
			}

			return fmt.Errorf("unable to update object: %w", err)
		}
	}

	return nil
}
