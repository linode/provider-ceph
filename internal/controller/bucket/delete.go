package bucket

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return errors.New(errNotBucket)
	}

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	// There are two scenarios where the bucket status needs to be updated during a
	// Delete invocation:
	// 1. The caller attempts to delete the CR and an error occurs during the call to
	// the bucket's backends. In this case the bucket may be successfully deleted
	// from some backends, but not from others. As such, we must update the bucket CR
	// status accordingly as Delete has ultimately failed and the 'in-use' finalizer
	// will not be removed.
	// 2. The caller attempts to delete the bucket from it's backends without deleting
	// the bucket CR. This is done by setting the Disabled flag on the bucket
	// CR spec. If the deletion is successful or unsuccessful, the bucket CR status must be
	// updated.
	bucketBackends := newBucketBackends()

	if !c.backendStore.BackendsAreStored() {
		return errors.New(errNoS3BackendsStored)
	}

	g := new(errgroup.Group)

	activeBackends := bucket.Spec.Providers
	if len(activeBackends) == 0 {
		activeBackends = c.backendStore.GetAllActiveBackendNames()
	}

	for _, backendName := range activeBackends {
		bucketBackends.setBucketStatus(bucket.Name, backendName, v1alpha1.DeletingStatus)

		c.log.Info("Deleting bucket", "bucket name", bucket.Name, "backend name", backendName)
		cl := c.backendStore.GetBackendClient(backendName)
		beName := backendName
		g.Go(func() error {
			var err error
			for i := 0; i < s3internal.RequestRetries; i++ {
				if err = s3internal.DeleteBucket(ctx, cl, aws.String(bucket.Name)); err == nil {
					bucketBackends.deleteBackend(bucket.Name, beName)

					break
				}
			}

			return err
		})
	}

	if err := g.Wait(); err != nil {
		return errors.Wrap(err, errDeleteBucket)
	}

	err := c.updateBucketCR(ctx, bucket, func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
		controllerutil.RemoveFinalizer(bucketLatest, inUseFinalizer)

		return NeedsObjectUpdate
	}, func(_, bucketLatest *v1alpha1.Bucket) UpdateRequired {
		setBucketStatus(bucketLatest, bucketBackends)

		return NeedsStatusUpdate
	})
	if err != nil {
		c.log.Info("Failed to update Bucket CR before delete", "bucket_name", bucket.Name)
	}

	return err
}
