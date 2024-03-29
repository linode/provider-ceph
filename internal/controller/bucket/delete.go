package bucket

import (
	"context"
	"math"

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
	"github.com/linode/provider-ceph/internal/rgw"
)

//nolint:gocyclo,cyclop // Function requires numerous checks.
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

	providerNames := []string{}
	for backendName, value := range getAllBackendLabels(bucket, true) {
		// Skip disabled backends
		if value != True {
			c.log.Info("Skipping deletion of bucket on backend, disabled", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			continue
		}

		providerNames = append(providerNames, backendName)

		if backend, ok := bucket.Status.AtProvider.Backends[backendName]; !ok || backend == nil {
			c.log.Info("Skipping deletion of bucket on backend, missing status", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			continue
		} else if reason := backend.BucketCondition.Reason; reason != xpv1.ReasonAvailable {
			c.log.Info("Skipping deletion of bucket on backend, not available", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			continue
		}

		bucketBackends.setBucketCondition(bucket.Name, backendName, xpv1.Deleting())

		c.log.Info("Deleting bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)
		beName := backendName
		g.Go(func() error {
			cl, err := c.s3ClientHandler.GetS3Client(ctx, bucket, beName)
			if err != nil {
				bucketBackends.setBucketCondition(bucket.Name, beName, xpv1.Deleting().WithMessage(err.Error()))

				return err
			}

			if err := rgw.DeleteBucket(ctx, cl, aws.String(bucket.Name), false); err != nil {
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
		// Bucket status is unavailable at this point. Use math.MaxUint as minReplicas is irrelevant in this scenario.
		setBucketStatus(bucketLatest, bucketBackends, providerNames, math.MaxUint)

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

		// If the error is BucketNotEmpty error, the DeleteBucket operation should be failed
		// and the client should be able to use the bucket with non-empty buckends.
		if errors.Is(deleteErr, rgw.ErrBucketNotEmpty) {
			if err := c.updateBucketCR(ctx, bucket, func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
				c.log.Info("Change 'disabled' flag to false", consts.KeyBucketName, bucket.Name)

				bucketLatest.Spec.Disabled = false

				return NeedsObjectUpdate
			}); err != nil {
				err = errors.Wrap(err, errUpdateBucketCR)
				c.log.Info("Failed to change 'disabled' flag to false", consts.KeyBackendName, bucket.Name, "error", err.Error())
				traces.SetAndRecordError(span, err)

				return err
			}
		}

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
