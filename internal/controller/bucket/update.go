package bucket

import (
	"context"

	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

//nolint:gocognit,gocyclo,cyclop // Function requires numerous checks.
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

	if err := c.updateAll(ctx, bucket); err != nil {
		return managed.ExternalUpdate{}, err
	}

	err := c.updateObject(ctx, bucket,
		func(origBucket, bucket *v1alpha1.Bucket) UpdateRequired {
			bucket.Status.Conditions = origBucket.Status.Conditions
			bucket.Status.AtProvider.Backends = origBucket.Status.AtProvider.Backends

			return NeedsStatusUpdate
		},
		func(origBucket, bucket *v1alpha1.Bucket) UpdateRequired {
			bucket.Spec.Providers = origBucket.Spec.Providers

			allBucketsReady := true
			for _, p := range bucket.Spec.Providers {
				if _, ok := bucket.Status.AtProvider.Backends[p]; !ok || bucket.Status.AtProvider.Backends[p].BucketStatus != v1alpha1.ReadyStatus {
					allBucketsReady = false

					break
				}
				if bucket.Status.AtProvider.Backends[p].BucketStatus != v1alpha1.ReadyStatus {
					allBucketsReady = false

					break
				}
			}

			if allBucketsReady &&
				(bucket.Spec.AutoPause || c.autoPauseBucket) &&
				bucket.Labels[meta.AnnotationKeyReconciliationPaused] != "true" {
				c.log.Info("Auto pausing bucket", "bucket_name", bucket.Name)

				if bucket.ObjectMeta.Labels == nil {
					bucket.ObjectMeta.Labels = map[string]string{}
				}
				bucket.Labels[meta.AnnotationKeyReconciliationPaused] = "true"
			}

			// Add labels for backends if they don't exist
			for _, beName := range bucket.Spec.Providers {
				beLabel := v1alpha1.BackendLabelPrefix + beName
				if _, ok := bucket.ObjectMeta.Labels[beLabel]; !ok {
					if bucket.ObjectMeta.Labels == nil {
						bucket.ObjectMeta.Labels = map[string]string{}
					}
					bucket.ObjectMeta.Labels[beLabel] = ""
				}
			}

			controllerutil.AddFinalizer(bucket, inUseFinalizer)

			return NeedsObjectUpdate
		})
	if err != nil {
		c.log.Info("Failed to update bucket", "bucket_name", bucket.Name)
	}

	return managed.ExternalUpdate{}, err
}

func (c *external) updateAll(ctx context.Context, bucket *v1alpha1.Bucket) error {
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
			c.log.Info("Backend is marked inactive - bucket will not be updated on backend", "bucket name", bucket.Name, "backend name", backendName)

			continue
		}

		cl := c.backendStore.GetBackendClient(backendName)
		if cl == nil {
			c.log.Info("Backend client not found for backend - bucket cannot be updated on backend", "bucket name", bucket.Name, "backend name", backendName)

			continue
		}

		beName := backendName
		g.Go(func() error {
			// Set the Bucket status to 'NotReady' until we have successfully performed the update.
			bucketBackends.setBucketStatus(bucket.Name, beName, v1alpha1.NotReadyStatus)

			for i := 0; i < s3internal.RequestRetries; i++ {
				c.log.Info("Updating bucket on backend", "bucket_name", bucket.Name, "backend_name", beName)
				bucketExists, err := s3internal.BucketExists(ctx, cl, bucket.Name)
				if err != nil {
					c.log.Info("Error occurred attempting HeadBucket", "err", err.Error(), "bucket_name", bucket.Name, "backend_ name", beName)

					return err
				}
				if !bucketExists {
					bucketBackends.deleteBackend(bucket.Name, beName)

					return nil
				}

				err = c.update(ctx, bucket, beName, bucketBackends)
				if err != nil {
					c.log.Info("Error occurred attempting to update bucket", "err", err.Error(), "bucket_name", bucket.Name, "backend_ name", beName)

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

func (c *external) update(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
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
