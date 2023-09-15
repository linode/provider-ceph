package bucket

import (
	"context"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/types"

	"github.com/pkg/errors"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	s3internal "github.com/linode/provider-ceph/internal/s3"
	"github.com/linode/provider-ceph/pkg/utils"
)

//nolint:maintidx,gocognit,gocyclo,cyclop,nolintlint // Function requires numerous checks.
func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotBucket)
	}

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	if bucket.Spec.Disabled {
		c.log.Info("Bucket is disabled - no buckets to be created on backends", "bucket name", bucket.Name)

		return managed.ExternalCreation{}, nil
	}

	if !c.backendStore.BackendsAreStored() {
		return managed.ExternalCreation{}, errors.New(errNoS3BackendsStored)
	}

	if len(bucket.Spec.Providers) == 0 {
		bucket.Spec.Providers = c.backendStore.GetAllActiveBackendNames()
	}

	// Create the bucket on each backend in a separate go routine
	activeBackends := c.backendStore.GetActiveBackends(bucket.Spec.Providers)
	if len(activeBackends) == 0 {
		return managed.ExternalCreation{}, errors.New(errNoS3BackendsRegistered)
	} else if len(activeBackends) != len(bucket.Spec.Providers) {
		return managed.ExternalCreation{}, errors.New(errMissingS3Backend)
	}

	updated := atomic.Bool{}
	errorsLeft := 0
	errChan := make(chan error, len(activeBackends))
	readyChan := make(chan string)

	for beName := range activeBackends {
		originalBucket := bucket.DeepCopy()

		cl := c.backendStore.GetBackendClient(beName)
		if cl == nil {
			c.log.Info("Backend client not found for backend - bucket cannot be created on backend", "bucket name", originalBucket.Name, "backend name", beName)

			continue
		}

		c.log.Info("Creating bucket", "bucket name", originalBucket.Name, "backend name", beName)

		pc := &apisv1alpha1.ProviderConfig{}
		if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: beName}, pc); err != nil {
			return managed.ExternalCreation{}, errors.Wrap(err, errGetPC)
		}

		if utils.IsHealthCheckBucket(bucket) && pc.Spec.DisableHealthCheck {
			c.log.Info("Health check is disabled on backend - health-check-bucket will not be created", "backend name", beName)

			continue
		}

		errorsLeft++

		beName := beName
		go func() {
			var err error

			for i := 0; i < s3internal.RequestRetries; i++ {
				_, err = cl.CreateBucket(ctx, s3internal.BucketToCreateBucketInput(originalBucket))
				if resource.Ignore(isAlreadyExists, err) == nil {
					break
				}
			}

			if err != nil {
				c.log.Info("Failed to create bucket on backend", "backend name", beName, "bucket_name", originalBucket.Name, "err", err.Error())

				errChan <- err

				return
			}

			if !updated.CompareAndSwap(false, true) {
				c.log.Info("Bucket already updated", "bucket_name", originalBucket.Name)

				errChan <- nil

				return
			}

			readyChan <- beName
			errChan <- nil
		}()
	}

	if errorsLeft == 0 {
		c.log.Info("Failed to find any backend for bucket", "bucket_name", bucket.Name)

		return managed.ExternalCreation{}, nil
	}

	return c.waitForCreation(ctx, bucket, readyChan, errChan, errorsLeft)
}

func (c *external) waitForCreation(ctx context.Context, bucket *v1alpha1.Bucket, readyChan <-chan string, errChan <-chan error, errorsLeft int) (managed.ExternalCreation, error) {
	var err error

WAIT:
	for {
		select {
		case <-ctx.Done():
			c.log.Info("Context timeout", "bucket_name", bucket.Name)

			return managed.ExternalCreation{}, ctx.Err()
		case beName := <-readyChan:
			c.log.Info("Bucket created", "backend name", beName, "bucket_name", bucket.Name)

			err := c.updateObject(ctx, bucket, func(_, bucket *v1alpha1.Bucket) bool {
				bucket.Status.SetConditions(xpv1.Available())
				bucket.Status.AtProvider.BackendStatuses = v1alpha1.BackendStatuses{
					beName: v1alpha1.BackendReadyStatus,
				}

				return true
			})
			if err != nil {
				c.log.Info("Failed to update backend status", "backend name", beName, "bucket_name", bucket.Name)
			}

			return managed.ExternalCreation{}, err
		case err = <-errChan:
			errorsLeft--

			if err != nil {
				c.log.Info("Failed to create on backend", "bucket_name", bucket.Name)

				if errorsLeft > 0 {
					continue
				}

				break WAIT
			}
		}
	}

	err = c.updateObject(ctx, bucket, func(_, bucket *v1alpha1.Bucket) bool {
		bucket.Status.SetConditions(xpv1.Unavailable())

		return true
	})
	if err != nil {
		c.log.Info("Failed to update backend unavailable status", "bucket_name", bucket.Name)
	}

	return managed.ExternalCreation{}, err
}
