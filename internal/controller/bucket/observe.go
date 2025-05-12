package bucket

import (
	"context"

	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"

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
func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.external.Observe")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, c.log)

	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		err := errors.New(errNotBucket)
		traces.SetAndRecordError(span, err)

		return managed.ExternalObservation{}, err
	}

	if !c.backendStore.BackendsAreStored() {
		err := errors.New(errNoS3BackendsStored)
		traces.SetAndRecordError(span, err)

		return managed.ExternalObservation{}, err
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

	if !bucket.Status.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) {
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	}

	providerNames := getBucketProvidersFilterDisabledLabel(bucket, c.backendStore.GetAllBackendNames())
	if len(providerNames) == 0 {
		err := errors.New(errAllS3BackendsDisabled)
		traces.SetAndRecordError(span, err)

		return managed.ExternalObservation{}, err
	}
	backendClients := c.backendStore.GetBackendS3Clients(providerNames)

	// Check that the Bucket CR is Available according to its Status backends.
	if !isBucketAvailableFromStatus(bucket, providerNames, backendClients) {
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	}

	// Observe sub-resources for the Bucket to check if they too are up to date.
	for _, subResourceClient := range c.subresourceClients {
		obs, err := subResourceClient.Observe(ctx, bucket, providerNames)
		if err != nil {
			err := errors.Wrap(err, errObserveSubresource)
			traces.SetAndRecordError(span, err)

			return managed.ExternalObservation{}, err
		}
		if obs == NeedsUpdate || obs == NeedsDeletion {
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
	for beName, backendClient := range backendClients {
		g.Go(func() error {
			bucketExists, err := rgw.BucketExists(ctxC, backendClient, bucket.Name)
			if err != nil {
				traces.SetAndRecordError(span, err)

				log.Info("Error observing bucket on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName, "error", err.Error())

				// If we have a connectivity issue it doesn't make sense to reconcile the bucket immediately.
				return nil
			} else if !bucketExists {
				err := errors.New("bucket does not exist")
				log.Info("Bucket not found on backend during observation", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)
				traces.SetAndRecordError(span, err)

				return err
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
