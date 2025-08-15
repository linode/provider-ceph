package bucket

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"
)

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.external.Update")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, c.log)

	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		err := errors.New(errNotBucket)
		traces.SetAndRecordError(span, err)

		return managed.ExternalUpdate{}, err
	}

	span.SetAttributes(attribute.String("bucket", bucket.Name))

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	// A disabled Bucket CR means we perform the Delete flow to remove buckets
	// from all backends without deleting the Bucket CR.
	if bucket.Spec.Disabled {
		log.Info("Bucket is disabled - remove any existing buckets from backends", "bucket name", bucket.Name)
		_, err := c.Delete(ctx, mg)

		return managed.ExternalUpdate{}, err
	}

	// allBackendNames is a list of the names of all backends from backend store.
	allBackendNames := c.backendStore.GetAllBackendNames()
	if len(allBackendNames) == 0 {
		err := errors.New(errNoS3BackendsStored)
		traces.SetAndRecordError(span, err)

		return managed.ExternalUpdate{}, err
	}

	// backendsToUpdateOnNames is a list of names of all backends on which this S3 bucket
	// is to be updated. This will either be:
	// 1. The list of bucket.Spec.Providers, if specified.
	// 2. Otherwise, the allBackendNames list.
	// In either case, the list will exclude any backends which have been specified as
	// disabled on the Bucket CR. A backend is specified as disabled for a given bucket
	// if it has been given the backend label (eg 'provider-ceph.backends.backend-a: "false"').
	// This means that Provider Ceph should NOT update the bucket on this backend.
	backendsToUpdateOnNames := getBucketProvidersFilterDisabledLabel(bucket, allBackendNames)
	if len(backendsToUpdateOnNames) == 0 {
		err := errors.New(errAllS3BackendsDisabled)
		traces.SetAndRecordError(span, err)

		return managed.ExternalUpdate{}, err
	}

	bucketBackends := newBucketBackends()
	updateAllErr := c.updateOnAllBackends(ctx, bucket, bucketBackends, backendsToUpdateOnNames)
	if updateAllErr != nil {
		log.Info("Failed to update on all backends", consts.KeyBucketName, bucket.Name, "error", updateAllErr.Error())
		traces.SetAndRecordError(span, updateAllErr)
	}

	// Whether buckets are updated successfully or not on backends, we need to update the
	// Bucket CR Status in all cases to represent the conditions of each individual bucket.
	if err := c.updateBucketCR(ctx, bucket,
		func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
			setBucketStatus(bucketLatest, bucketBackends, backendsToUpdateOnNames, c.minReplicas)

			return NeedsStatusUpdate
		}); err != nil {
		log.Info("Failed to update Bucket CR Status", consts.KeyBucketName, bucket.Name, "error", err.Error())
		traces.SetAndRecordError(span, err)

		return managed.ExternalUpdate{}, err
	}

	// The buckets have been updated successfully on all backends, so we need to update the
	// Bucket CR Spec accordingly.
	err := c.updateBucketCR(ctx, bucket,
		func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
			if bucketLatest.Labels == nil {
				bucketLatest.Labels = map[string]string{}
			}

			// Auto pause the Bucket CR if required - ie if auto-pause has been enabled and the
			// criteria is met before pausing a Bucket CR.
			if isPauseRequired(
				bucketLatest,
				backendsToUpdateOnNames,
				c.backendStore.GetBackendS3Clients(backendsToUpdateOnNames),
				bucketBackends,
				c.autoPauseBucket,
			) {
				log.Info("Auto pausing bucket", consts.KeyBucketName, bucket.Name)
				bucketLatest.Labels[meta.AnnotationKeyReconciliationPaused] = True
			}
			// Apply the backend label to the Bucket CR for each backend that the bucket was
			// intended to be updated on.
			setAllBackendLabels(bucketLatest, backendsToUpdateOnNames)

			return NeedsObjectUpdate
		})
	if err != nil {
		log.Info("Failed to update Bucket CR Spec", consts.KeyBucketName, bucket.Name, "error", err.Error())
		traces.SetAndRecordError(span, err)

		return managed.ExternalUpdate{}, err
	}

	return managed.ExternalUpdate{}, updateAllErr
}

func (c *external) updateOnAllBackends(ctx context.Context, bucket *v1alpha1.Bucket, bb *bucketBackends, backendsToUpdateOnNames []string) error {
	ctx, span := otel.Tracer("").Start(ctx, "updateOnAllBackends")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, c.log)

	defer setBucketStatus(bucket, bb, backendsToUpdateOnNames, c.minReplicas)

	g := new(errgroup.Group)

	for _, backendName := range backendsToUpdateOnNames {
		// Attempt to get an S3 client for the backend. This will either be the default
		// S3 client created for each backend by the backend monitor or it will be a new
		// temporary S3 client created via the STS AssumeRole endpoint. The latter will
		// be used if the user has specified an "assume-role-arn" at start-up. If an error
		// occurs, update the Bucket CR Status with the condition of this backend and
		// continue to the next backend.
		cl, err := c.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
		if err != nil {
			traces.SetAndRecordError(span, err)
			log.Info("Failed to get client for backend - bucket cannot be updated on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName, "error", err.Error())
			bb.setBucketCondition(bucket.Name, backendName, xpv1.Unavailable().WithMessage(err.Error()))

			continue
		}

		g.Go(c.updateOnBackend(ctx, backendName, bucket, cl, bb))
	}

	if err := g.Wait(); err != nil {
		traces.SetAndRecordError(span, err)

		return err
	}

	return nil
}

func (c *external) updateOnBackend(ctx context.Context, beName string, bucket *v1alpha1.Bucket, cl backendstore.S3Client, bb *bucketBackends) func() error {
	ctx, log := traces.InjectTraceAndLogger(ctx, c.log)

	return func() error {
		log.Info("Updating bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
		bucketExists, err := rgw.BucketExists(ctx, cl, bucket.Name)
		if err != nil {
			log.Info("Error occurred attempting HeadBucket", "err", err.Error(), consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
			bb.setBucketCondition(bucket.Name, beName, xpv1.Unavailable().WithMessage(err.Error()))

			return err
		}
		if !bucketExists {
			if !c.recreateMissingBucket {
				bb.deleteBackend(bucket.Name, beName)

				return nil
			}

			_, err := rgw.CreateBucket(ctx, cl, rgw.BucketToCreateBucketInput(bucket))
			if err != nil {
				log.Info("Failed to recreate missing bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName, "err", err.Error())

				return err
			}
			log.Info("Recreated missing bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
		}

		err = c.doUpdateOnBackend(ctx, bucket, beName, bb)
		if err != nil {
			log.Info("Error occurred attempting to update bucket", "err", err.Error(), consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
			bb.setBucketCondition(bucket.Name, beName, xpv1.Unavailable().WithMessage(err.Error()))

			return err
		}
		// Check if this backend has been marked as 'Unhealthy'. In which case the
		// bucket condition must remain in 'Unavailable' for this backend.
		if c.backendStore.GetBackendHealthStatus(beName) == apisv1alpha1.HealthStatusUnhealthy {
			bb.setBucketCondition(bucket.Name, beName, xpv1.Unavailable().WithMessage("Backend is marked Unhealthy"))

			return nil
		}
		// Bucket has been successfully updated and the backend is either 'Healthy' or 'Unknown'.
		// It may be 'Unknown' due to the healthcheck being disabled, in which case we can only assume
		// the backend is healthy. Either way, set the bucket condition as 'Available' for this backend.
		bb.setBucketCondition(bucket.Name, beName, xpv1.Available())

		return nil
	}
}

func (c *external) doUpdateOnBackend(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	for _, subResourceClient := range c.subresourceClients {
		err := subResourceClient.Handle(ctx, b, backendName, bb)
		if err != nil {
			return errors.Wrap(err, errHandleSubresource)
		}
	}

	return nil
}
