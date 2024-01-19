package bucket

import (
	"context"

	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"

	"github.com/aws/aws-sdk-go-v2/aws"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/otel/traces"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.external.Delete")
	defer span.End()

	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		err := errors.New(errNotBucket)
		traces.SetAndRecordError(span, err)

		return err
	}

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	bucketBackends := newBucketBackends()

	if !c.backendStore.BackendsAreStored() {
		err := errors.New(errNoS3BackendsStored)
		traces.SetAndRecordError(span, err)

		return err
	}

	g := new(errgroup.Group)

	activeBackends := bucket.Spec.Providers
	if len(activeBackends) == 0 {
		activeBackends = c.backendStore.GetAllActiveBackendNames()
	}

	for _, backendName := range activeBackends {
		bucketBackends.setBucketCondition(bucket.Name, backendName, xpv1.Deleting())

		c.log.Info("Deleting bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)
		cl := c.backendStore.GetBackendClient(backendName)
		beName := backendName
		g.Go(func() error {
			if err := s3internal.DeleteBucket(ctx, cl, aws.String(bucket.Name)); err != nil {
				bucketBackends.setBucketCondition(bucket.Name, beName, xpv1.Deleting().WithMessage(err.Error()))

				return err
			}
			bucketBackends.deleteBackend(bucket.Name, beName)

			return nil
		})
	}

	// Wait for all go routines to finish
	deleteErr := g.Wait()

	// Regardless of whether an error occurred, we need to update the Bucket CR Status
	// to cover the following scenarios:
	// 1. The caller attempts to delete the CR and an error occurs during the call to
	// the bucket's backends. In this case the bucket may be successfully deleted
	// from some backends, but not from others. As such, we must update the bucket CR
	// status accordingly as Delete has ultimately failed and the 'in-use' finalizer
	// will not be removed.
	// 2. The caller attempts to delete the bucket from it's backends without deleting
	// the bucket CR. This is done by setting the Disabled flag on the bucket
	// CR spec. If the deletion is successful or unsuccessful, the bucket CR status must be
	// updated.
	if err := c.updateBucketCR(ctx, bucket, func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
		bucketLatest.Spec.Providers = activeBackends
		setBucketStatus(bucketLatest, bucketBackends)

		return NeedsStatusUpdate
	}); err != nil {
		err = errors.Wrap(err, errUpdateBucketCR)
		c.log.Info("Failed to update Bucket Status after attempting to delete bucket from backends", consts.KeyBucketName, bucket.Name, "error", err.Error())
		traces.SetAndRecordError(span, err)

		return err
	}

	// If an error occurred during deletion, we must return for requeue.
	if deleteErr != nil {
		c.log.Info("Failed to delete bucket on one or more backends", "error", deleteErr.Error())
		traces.SetAndRecordError(span, deleteErr)

		return deleteErr
	}

	c.log.Info("All buckets successfully deleted from backends for Bucket CR", consts.KeyBucketName, bucket.Name)

	// No errors occurred - the bucket has successfully been deleted from all backends.
	// We do not need to update the Bucket CR Status, we simply remove the "in-use" finalizer.
	if err := c.updateBucketCR(ctx, bucket, func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
		c.log.Info("Removing 'in-use' finalizer from Bucket CR", consts.KeyBucketName, bucket.Name)

		controllerutil.RemoveFinalizer(bucketLatest, v1alpha1.InUseFinalizer)

		return NeedsObjectUpdate
	}); err != nil {
		err = errors.Wrap(err, errUpdateBucketCR)
		c.log.Info("Failed to remove 'in-use' finalizer from Bucket CR", consts.KeyBucketName, bucket.Name, "error", err.Error())
		traces.SetAndRecordError(span, err)

		return err
	}

	return nil
}
