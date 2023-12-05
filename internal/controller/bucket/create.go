package bucket

import (
	"context"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/types"

	"github.com/pkg/errors"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	s3internal "github.com/linode/provider-ceph/internal/s3"
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
		c.log.Info("Bucket is disabled - no buckets to be created on backends", "bucket_name", bucket.Name)

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

	// This value shows a bucket on one backend is already created.
	// It is used to prevent goroutines from sending duplicated messages to `readyChan`.
	bucketAlreadyCreated := atomic.Bool{}
	backendCount := 0
	errChan := make(chan error, len(activeBackends))
	readyChan := make(chan string)

	for beName := range activeBackends {
		originalBucket := bucket.DeepCopy()

		cl := c.backendStore.GetBackendClient(beName)
		if cl == nil {
			c.log.Info("Backend client not found for backend - bucket cannot be created on backend", "bucket_name", originalBucket.Name, "backend_name", beName)

			continue
		}

		c.log.Info("Creating bucket on backend", "bucket_name", originalBucket.Name, "backend_name", beName)

		pc := &apisv1alpha1.ProviderConfig{}
		if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: beName}, pc); err != nil {
			c.log.Info("Failed to fetch provider config", "bucket_name", originalBucket.Name, "backend_name", beName, "err", err.Error())

			return managed.ExternalCreation{}, errors.Wrap(err, errGetPC)
		}

		// Increment the backend counter. We need this later to know when the operation should finish.
		backendCount++

		beName := beName
		go func() {
			var err error

			for i := 0; i < s3internal.RequestRetries; i++ {
				_, err = s3internal.CreateBucket(ctx, cl, s3internal.BucketToCreateBucketInput(originalBucket))
				if resource.Ignore(s3internal.IsAlreadyExists, err) == nil {
					c.log.Info("Bucket created on backend", "bucket_name", bucket.Name, "backend_name", beName)

					break
				}
			}
			if err != nil {
				c.log.Info("Failed to create bucket on backend", "bucket_name", originalBucket.Name, "backend_name", beName, "err", err.Error())

				errChan <- err

				return
			}

			// This compare-and-swap operation is the atomic equivalent of:
			//	if *bucketAlreadyCreated == false {
			//		*bucketAlreadyCreated = true
			//		return true
			//	}
			//	return false
			if !bucketAlreadyCreated.CompareAndSwap(false, true) {
				c.log.Info("Bucket already created on backend - terminate thread without error", "bucket_name", originalBucket.Name, "backend_name", beName)

				errChan <- nil

				return
			}

			// Once a bucket is created successfully on ANY backend, the bucket is considered ready.
			// Therefore we send the name of the backend on which the bucket is first created to the ready channel.
			readyChan <- beName
			errChan <- nil
		}()
	}

	if backendCount == 0 {
		c.log.Info("Failed to find any backend for bucket", "bucket_name", bucket.Name)

		return managed.ExternalCreation{}, nil
	}

	return c.waitForCreationAndUpdateBucketCR(ctx, bucket, readyChan, errChan, backendCount)
}

func (c *external) waitForCreationAndUpdateBucketCR(ctx context.Context, bucket *v1alpha1.Bucket, readyChan <-chan string, errChan <-chan error, backendCount int) (managed.ExternalCreation, error) {
	var err error

	for i := 0; i < backendCount; i++ {
		select {
		case <-ctx.Done():
			c.log.Info("Context timeout waiting for bucket creation", "bucket_name", bucket.Name)

			return managed.ExternalCreation{}, ctx.Err()
		case beName := <-readyChan:
			err := c.updateBucketCR(ctx, bucket, func(_, bucketLatest *v1alpha1.Bucket) UpdateRequired {
				// Remove the annotation, because Crossplane is not always able to do it.
				// This workaround doesn't eliminates the problem, if this update fails,
				// Crossplane skips object forever.
				delete(bucketLatest.ObjectMeta.Annotations, meta.AnnotationKeyExternalCreatePending)

				// Add labels for the backend
				if bucketLatest.ObjectMeta.Labels == nil {
					bucketLatest.ObjectMeta.Labels = map[string]string{}
				}
				bucketLatest.ObjectMeta.Labels[v1alpha1.BackendLabelPrefix+beName] = ""

				return NeedsObjectUpdate
			}, func(_, bucketLatest *v1alpha1.Bucket) UpdateRequired {
				bucketLatest.Status.SetConditions(xpv1.Available())
				bucketLatest.Status.AtProvider.Backends = v1alpha1.Backends{
					beName: &v1alpha1.BackendInfo{
						BucketStatus: v1alpha1.ReadyStatus,
					},
				}

				return NeedsStatusUpdate
			})
			if err != nil {
				c.log.Info("Failed to update Bucket CR with backend info", "bucket_name", bucket.Name, "backend_name", beName, "err", err.Error())
			}

			return managed.ExternalCreation{}, err
		case <-errChan:
			continue
		}
	}

	c.log.Info("Failed to create bucket on any backend", "bucket_name", bucket.Name)

	err = c.updateBucketCR(ctx, bucket, func(_, bucketLatest *v1alpha1.Bucket) UpdateRequired {
		bucketLatest.Status.SetConditions(xpv1.Unavailable())

		return NeedsStatusUpdate
	})
	if err != nil {
		c.log.Info("Failed to update backend unavailable status on Bucket CR", "bucket_name", bucket.Name)
	}

	return managed.ExternalCreation{}, err
}
