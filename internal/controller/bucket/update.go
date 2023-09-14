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
	"github.com/linode/provider-ceph/internal/backendstore"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

//nolint:gocyclo,cyclop // Function requires numerous checks.
func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotBucket)
	}

	if v1alpha1.IsHealthCheckBucket(bucket) {
		c.log.Info("Update is NOOP for health check bucket - updates performed by health-check-controller", "bucket", bucket.Name)

		return managed.ExternalUpdate{}, nil
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
			bucket.Status.AtProvider.BackendStatuses = origBucket.Status.AtProvider.BackendStatuses

			return NeedsStatusUpdate
		},
		func(origBucket, bucket *v1alpha1.Bucket) UpdateRequired {
			bucket.Spec.Providers = origBucket.Spec.Providers

			allBucketsReady := true
			for _, p := range bucket.Spec.Providers {
				if bucket.Status.AtProvider.BackendStatuses[p] != v1alpha1.BackendReadyStatus {
					allBucketsReady = false

					break
				}
			}

			if !v1alpha1.IsHealthCheckBucket(bucket) &&
				allBucketsReady &&
				(bucket.Spec.AutoPause || c.autoPauseBucket) &&
				bucket.Annotations[meta.AnnotationKeyReconciliationPaused] == "" {
				c.log.Info("Auto pausing bucket", "bucket_name", bucket.Name)
				bucket.Annotations[meta.AnnotationKeyReconciliationPaused] = "true"
			}

			// Add labels for backends if they don't exist
			for _, beName := range bucket.Spec.Providers {
				if _, ok := bucket.ObjectMeta.Labels[beName]; !ok {
					if bucket.ObjectMeta.Labels == nil {
						bucket.ObjectMeta.Labels = map[string]string{}
					}
					bucket.ObjectMeta.Labels[beName] = ""
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

		c.log.Info("Updating bucket", "bucket name", bucket.Name, "backend name", backendName)

		beName := backendName
		g.Go(func() error {
			bucketBackends.setBucketBackendStatus(bucket.Name, beName, v1alpha1.NotReadyStatus)

			for i := 0; i < s3internal.RequestRetries; i++ {
				bucketExists, err := s3internal.BucketExists(ctx, cl, bucket.Name)
				if err != nil {
					return err
				}
				if !bucketExists {
					bucketBackends.deleteBucketBackend(bucket.Name, beName)

					return nil
				}

				bucketBackends.setBucketBackendStatus(bucket.Name, beName, v1alpha1.NotReadyStatus)

				err = c.update(ctx, bucket, cl)
				if err == nil {
					// Check to see if this backend has been marked as 'Unhealthy'. It may be 'Unknown' due to
					// the healthcheck being disabled. In which case we can only assume the backend is healthy
					// and mark the bucket as 'Ready' for this backend.
					if c.backendStore.GetBackendHealthStatus(beName) == apisv1alpha1.HealthStatusUnhealthy {
						break
					}

					bucketBackends.setBucketBackendStatus(bucket.Name, beName, v1alpha1.ReadyStatus)
				}
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return errors.Wrap(err, errUpdateBucket)
	}

	return nil
}

func (c *external) update(ctx context.Context, bucket *v1alpha1.Bucket, s3Backend backendstore.S3Client) error {
	if s3types.ObjectOwnership(aws.ToString(bucket.Spec.ForProvider.ObjectOwnership)) == s3types.ObjectOwnershipBucketOwnerEnforced {
		_, err := s3Backend.PutBucketAcl(ctx, s3internal.BucketToPutBucketACLInput(bucket))
		if err != nil {
			return err
		}
	}

	//TODO: Add functionality for bucket ownership controls, using s3 apis:
	// - DeleteBucketOwnershipControls
	// - PutBucketOwnershipControls

	return nil
}
