package bucket

import (
	"context"

	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

//nolint:gocyclo,cyclop // Function requires numerous checks.
func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotBucket)
	}

	if !c.backendStore.BackendsAreStored() {
		return managed.ExternalObservation{}, errors.New(errNoS3BackendsStored)
	}

	if len(bucket.Status.AtProvider.Backends) == 0 {
		return managed.ExternalObservation{
			ResourceExists:   false,
			ResourceUpToDate: true,
		}, nil
	}

	if (bucket.Spec.AutoPause || c.autoPauseBucket) && !isBucketPaused(bucket) {
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	}

	if !controllerutil.ContainsFinalizer(bucket, inUseFinalizer) {
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	}

	if !isBucketAvailable(bucket) {
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	}

	// If no Providers are specified in the Bucket Spec, the bucket is to be created on all backends.
	if len(bucket.Spec.Providers) == 0 {
		bucket.Spec.Providers = c.backendStore.GetAllActiveBackendNames()
	}
	backendClients := c.backendStore.GetBackendClients(bucket.Spec.Providers)

	// Check that the bucket is Ready on the desired backends.
	if !isBucketReadyOnBackends(bucket, backendClients) {
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	}

	// Observe sub-resources for the Bucket to check if they too are up to date.
	for _, subResourceClient := range c.subresourceClients {
		obs, err := subResourceClient.Observe(ctx, bucket, bucket.Spec.Providers)
		if err != nil {
			return managed.ExternalObservation{}, err
		}
		if obs != Updated {
			return managed.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: false,
			}, nil
		}
	}

	// Create a new context and cancel it when we have either found the bucket
	// somewhere or cannot find it anywhere.
	ctxC, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	g := new(errgroup.Group)

	// Check for the bucket on each backend in a separate go routine
	for _, backendClient := range backendClients {
		backendClient := backendClient
		g.Go(func() error {
			bucketExists, err := s3internal.BucketExists(ctxC, backendClient, bucket.Name)
			if err != nil {
				c.log.Info(errors.Wrap(err, errGetBucket).Error())

				// If we have a connectivity issue it doesn't make sense to reconcile the bucket immediately.
				return nil
			} else if !bucketExists {
				return errors.New("missing bucket")
			}

			return nil
		})
	}

	// If the bucket is disabled, or if we have received an error finding the bucket on a backend,
	// then the Bucket can be considered NOT up to date.
	// A disabled bucket is considered not up to date so that Update can be performed next to
	// perform the necessary cleanup.
	resourceUpToDate := !bucket.Spec.Disabled
	if err := g.Wait(); err != nil {
		resourceUpToDate = false
	}

	return managed.ExternalObservation{
		// Return false when the external resource does not exist. This lets
		// the managed resource reconciler know that it needs to call Create to
		// (re)create the resource, or that it has successfully been deleted.
		ResourceExists: true,

		// Return false when the external resource exists, but it not up to date
		// with the desired managed resource state. This lets the managed
		// resource reconciler know that it needs to call Update.
		ResourceUpToDate: resourceUpToDate,

		// Return any details that may be required to connect to the external
		// resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}
