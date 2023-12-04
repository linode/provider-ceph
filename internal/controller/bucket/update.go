package bucket

import (
	"context"

	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/consts"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotBucket)
	}

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	if bucket.Spec.Disabled {
		c.log.Info("Bucket is disabled - remove any existing buckets from backends", "bucket name", bucket.Name)

		return managed.ExternalUpdate{}, c.Delete(ctx, mg)
	}

	if len(bucket.Spec.Providers) == 0 {
		bucket.Spec.Providers = c.backendStore.GetAllActiveBackendNames()
	}

	if err := c.updateOnAllBackends(ctx, bucket); err != nil {
		return managed.ExternalUpdate{}, err
	}

	err := c.updateBucketCR(ctx, bucket,
		func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
			bucketLatest.Status.Conditions = bucketDeepCopy.Status.Conditions
			bucketLatest.Status.AtProvider.Backends = bucketDeepCopy.Status.AtProvider.Backends

			return NeedsStatusUpdate
		},
		func(bucketDeepCopy, bucketLatest *v1alpha1.Bucket) UpdateRequired {
			bucketLatest.Spec.Providers = bucketDeepCopy.Spec.Providers

			// Auto pause the Bucket CR if required.
			cls := c.backendStore.GetBackendClients(bucketLatest.Spec.Providers)
			if isPauseRequired(bucketLatest, isBucketReadyOnBackends(bucket, cls), c.autoPauseBucket) {
				c.log.Info("Auto pausing bucket", consts.KeyBucketName, bucket.Name)
				pauseBucket(bucketLatest)
			}

			// Add labels for backends if they don't exist.
			setBackendLabels(bucket)

			controllerutil.AddFinalizer(bucketLatest, inUseFinalizer)

			return NeedsObjectUpdate
		})
	if err != nil {
		c.log.Info("Failed to update Bucket CR", consts.KeyBucketName, bucket.Name)
	}

	return managed.ExternalUpdate{}, err
}

func (c *external) updateOnAllBackends(ctx context.Context, bucket *v1alpha1.Bucket) error {
	bucketBackends := newBucketBackends()
	defer setBucketStatus(bucket, bucketBackends)

	g := new(errgroup.Group)

	activeBackends := c.backendStore.GetActiveBackends(bucket.Spec.Providers)
	if len(activeBackends) == 0 {
		return errors.New(errNoS3BackendsRegistered)
	} else if len(activeBackends) != len(bucket.Spec.Providers) {
		return errors.New(errMissingS3Backend)
	}

	for backendName := range activeBackends {
		if !c.backendStore.IsBackendActive(backendName) {
			c.log.Info("Backend is marked inactive - bucket will not be updated on backend", "bucket_ name", bucket.Name, consts.KeyBackendName, backendName)

			continue
		}

		cl := c.backendStore.GetBackendClient(backendName)
		if cl == nil {
			c.log.Info("Backend client not found for backend - bucket cannot be updated on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			continue
		}

		beName := backendName
		g.Go(func() error {
			// Set the Bucket status to 'NotReady' until we have successfully performed the update.
			bucketBackends.setBucketStatus(bucket.Name, beName, v1alpha1.NotReadyStatus)

			for i := 0; i < s3internal.RequestRetries; i++ {
				c.log.Info("Updating bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
				bucketExists, err := s3internal.BucketExists(ctx, cl, bucket.Name)
				if err != nil {
					c.log.Info("Error occurred attempting HeadBucket", "err", err.Error(), consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)

					return err
				}
				if !bucketExists {
					bucketBackends.deleteBackend(bucket.Name, beName)

					return nil
				}

				err = c.updateOnBackend(ctx, bucket, beName, bucketBackends)
				if err != nil {
					c.log.Info("Error occurred attempting to update bucket", "err", err.Error(), consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)

					continue
				}
				// Check if this backend has been marked as 'Unhealthy'. In which case the
				// Bucket must remain in 'NotReady' state for this backend.
				if c.backendStore.GetBackendHealthStatus(beName) == apisv1alpha1.HealthStatusUnhealthy {
					return nil
				}
				// Bucket has been successfully updated and the backend is either 'Healthy' or 'Unknown'.
				// It may be 'Unknown' due to the healthcheck being disabled, in which case we can only assume
				// the backend is healthy. Either way, set the bucket status as 'Ready' for this backend.
				bucketBackends.setBucketStatus(bucket.Name, beName, v1alpha1.ReadyStatus)

				return nil
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return errors.Wrap(err, errUpdateBucket)
	}

	return nil
}

func (c *external) updateOnBackend(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	cl := c.backendStore.GetBackendClient(backendName)
	if s3types.ObjectOwnership(aws.ToString(b.Spec.ForProvider.ObjectOwnership)) == s3types.ObjectOwnershipBucketOwnerEnforced {
		_, err := cl.PutBucketAcl(ctx, s3internal.BucketToPutBucketACLInput(b))
		if err != nil {
			return err
		}
	}

	//TODO: Add functionality for bucket ownership controls, using s3 apis:
	// - DeleteBucketOwnershipControls
	// - PutBucketOwnershipControls

	for _, subResourceClient := range c.subresourceClients {
		err := subResourceClient.Handle(ctx, b, backendName, bb)
		if err != nil {
			return err
		}
	}

	return nil
}
