package bucket

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"
	"github.com/linode/provider-ceph/internal/utils"
)

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.external.Update")
	defer span.End()

	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		err := errors.New(errNotBucket)
		traces.SetAndRecordError(span, err)

		return managed.ExternalUpdate{}, err
	}

	span.SetAttributes(attribute.String("bucket", bucket.Name))

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	if bucket.Spec.Disabled {
		c.log.Info("Bucket is disabled - remove any existing buckets from backends", "bucket name", bucket.Name)

		return managed.ExternalUpdate{}, c.Delete(ctx, mg)
	}

	allBackendNames := c.backendStore.GetAllBackendNames(false)
	providerNames := getBucketProvidersFilterDisabledLabel(bucket, allBackendNames)

	activeBackends := c.backendStore.GetActiveBackends(providerNames)
	if len(activeBackends) == 0 {
		err := errors.New(errNoActiveS3Backends)
		traces.SetAndRecordError(span, err)

		return managed.ExternalUpdate{}, err
	}

	bucketBackends := newBucketBackends()

	updateAllErr := c.updateOnAllBackends(ctx, bucket, bucketBackends, providerNames)
	if updateAllErr != nil {
		c.log.Info("Failed to update on all backends", consts.KeyBucketName, bucket.Name, "error", updateAllErr.Error())
		traces.SetAndRecordError(span, updateAllErr)
	}

	// Whether buckets are updated successfully or not on backends, we need to update the
	// Bucket CR Status in all cases to represent the conditions of each individual bucket.
	if err := c.updateBucketCR(ctx, bucket,
		func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
			setBucketStatus(bucketLatest, bucketBackends, providerNames, c.minReplicas)

			return NeedsStatusUpdate
		}); err != nil {
		c.log.Info("Failed to update Bucket CR Status", consts.KeyBucketName, bucket.Name, "error", err.Error())
		traces.SetAndRecordError(span, err)

		return managed.ExternalUpdate{}, err
	}

	// The buckets have been updated successfully on all backends, so we need to update the
	// Bucket CR Spec accordingly.
	err := c.updateBucketCR(ctx, bucket,
		func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
			if bucketLatest.ObjectMeta.Labels == nil {
				bucketLatest.ObjectMeta.Labels = map[string]string{}
			}

			// Auto pause the Bucket CR if required.
			cls := c.backendStore.GetBackendS3Clients(providerNames)
			if isPauseRequired(bucketLatest, providerNames, c.minReplicas, cls, bucketBackends, c.autoPauseBucket) {
				c.log.Info("Auto pausing bucket", consts.KeyBucketName, bucket.Name)
				bucketLatest.Labels[meta.AnnotationKeyReconciliationPaused] = True
			} else if updateAllErr == nil && len(activeBackends) != len(providerNames) {
				updateAllErr = errors.New(errMissingS3Backend)
				c.log.Info("Missing S3 backends", consts.KeyBucketName, bucket.Name, "missing", utils.MissingStrings(providerNames, allBackendNames))
				traces.SetAndRecordError(span, updateAllErr)
			}

			setAllBackendLabels(bucketLatest, providerNames)

			controllerutil.AddFinalizer(bucketLatest, v1alpha1.InUseFinalizer)

			return NeedsObjectUpdate
		})
	if err != nil {
		c.log.Info("Failed to update Bucket CR Spec", consts.KeyBucketName, bucket.Name, "error", err.Error())
		traces.SetAndRecordError(span, err)

		return managed.ExternalUpdate{}, err
	}

	return managed.ExternalUpdate{}, updateAllErr
}

func (c *external) updateOnAllBackends(ctx context.Context, bucket *v1alpha1.Bucket, bb *bucketBackends, providerNames []string) error {
	ctx, span := otel.Tracer("").Start(ctx, "updateOnAllBackends")
	defer span.End()

	defer setBucketStatus(bucket, bb, providerNames, c.minReplicas)

	g := new(errgroup.Group)

	for backendName := range c.backendStore.GetActiveBackends(providerNames) {
		if !c.backendStore.IsBackendActive(backendName) {
			c.log.Info("Backend is marked inactive - bucket will not be updated on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)
			bb.setBucketCondition(bucket.Name, backendName, xpv1.Unavailable().WithMessage("Backend is marked inactive"))

			continue
		}

		cl, err := c.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
		if err != nil {
			traces.SetAndRecordError(span, err)
			c.log.Info("Failed to get client for backend - bucket cannot be updated on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName, "error", err.Error())
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
	return func() error {
		c.log.Info("Updating bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
		bucketExists, err := rgw.BucketExists(ctx, cl, bucket.Name)
		if err != nil {
			c.log.Info("Error occurred attempting HeadBucket", "err", err.Error(), consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
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
				c.log.Info("Failed to recreate missing bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName, "err", err.Error())

				return err
			}
			c.log.Info("Recreated missing bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
		}

		err = c.doUpdateOnBackend(ctx, cl, bucket, beName, bb)
		if err != nil {
			c.log.Info("Error occurred attempting to update bucket", "err", err.Error(), consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
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

func (c *external) doUpdateOnBackend(ctx context.Context, cl backendstore.S3Client, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	for _, subResourceClient := range c.subresourceClients {
		err := subResourceClient.Handle(ctx, b, backendName, bb)
		if err != nil {
			return errors.Wrap(err, errHandleSubresource)
		}
	}

	return nil
}
