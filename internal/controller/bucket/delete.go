package bucket

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"

	"github.com/aws/aws-sdk-go-v2/aws"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"
)

//nolint:gocyclo,cyclop // Function requires numerous checks.
func (c *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.external.Delete")
	defer span.End()

	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		err := errors.New(errNotBucket)
		traces.SetAndRecordError(span, err)

		return managed.ExternalDelete{}, err
	}
	span.SetAttributes(attribute.String(consts.KeyBucketName, bucket.Name))

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	bucketBackends := newBucketBackends()

	if !c.backendStore.BackendsAreStored() {
		err := errors.New(errNoS3BackendsStored)
		traces.SetAndRecordError(span, err)

		return managed.ExternalDelete{}, err
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
	// status accordingly as Delete has ultimately failed.
	// 2. The caller attempts to delete the bucket from it's backends without deleting
	// the bucket CR. This is done by setting the Disabled flag on the bucket
	// CR spec. If the deletion is successful or unsuccessful, the bucket CR status must be
	// updated.
	if err := c.updateBucketCR(ctx, bucket, func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
		setBucketStatus(bucketLatest, bucketBackends, providerNames, c.minReplicas)

		return NeedsStatusUpdate
	}); err != nil {
		err = errors.Wrap(err, errUpdateBucketCR)
		c.log.Info("Failed to update Bucket Status after attempting to delete bucket from backends", consts.KeyBucketName, bucket.Name, "error", err.Error())
		traces.SetAndRecordError(span, err)

		return managed.ExternalDelete{}, err
	}

	if deleteErr != nil { //nolint:nestif // Multiple checks required.
		c.log.Info("Failed to delete bucket on one or more backends", "error", deleteErr.Error())
		traces.SetAndRecordError(span, deleteErr)

		if errors.Is(deleteErr, rgw.ErrBucketNotEmpty) {
			c.log.Info("Cannot delete non-empty bucket - this error will not be requeued", consts.KeyBucketName, bucket.Name)
			// An error occurred attempting to delete the bucket because it is not empty.
			// If this Delete operation was triggered because the Bucket CR was "Disabled",
			// we need to unset this value so as not to continue attempting Delete.
			// Otherwise we can return no error as we do not wish to requeue the Delete.
			if !bucket.Spec.Disabled {
				return managed.ExternalDelete{}, nil
			}
			if err := c.updateBucketCR(ctx, bucket, func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
				c.log.Info("Bucket CRs with non-empty buckets should not be disabled - setting 'disabled' flag to false", consts.KeyBucketName, bucket.Name)

				bucketLatest.Spec.Disabled = false

				return NeedsObjectUpdate
			}); err != nil {
				err = errors.Wrap(err, errUpdateBucketCR)
				c.log.Info("Failed to set 'disabled' flag to false", consts.KeyBucketName, bucket.Name, "error", err.Error())
				traces.SetAndRecordError(span, err)

				return managed.ExternalDelete{}, err
			}

			return managed.ExternalDelete{}, nil
		}
		// In all other cases we should return the deletion error for requeue.
		return managed.ExternalDelete{}, deleteErr
	}

	c.log.Info("All buckets successfully deleted from backends for Bucket CR", consts.KeyBucketName, bucket.Name)

	return managed.ExternalDelete{}, nil
}
