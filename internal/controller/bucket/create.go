package bucket

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"
	"github.com/linode/provider-ceph/internal/utils"
)

//nolint:maintidx,gocognit,gocyclo,cyclop,nolintlint // Function requires numerous checks.
func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.external.Create")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, c.log)

	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		err := errors.New(errNotBucket)
		traces.SetAndRecordError(span, err)

		return managed.ExternalCreation{}, err
	}

	span.SetAttributes(attribute.String(consts.KeyBucketName, bucket.Name))

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	// A disabled Bucket CR means we log and return no error as we do not wish to requeue.
	if bucket.Spec.Disabled {
		log.Info("Bucket is disabled - no buckets to be created on backends", consts.KeyBucketName, bucket.Name)

		return managed.ExternalCreation{}, nil
	}

	// If no backends are stored then we return an error in order to requeue until
	// backends appear in the store. The backend store is updated by the backend-monitor
	// which reconciles ProviderConfig objects representing backends.
	if !c.backendStore.BackendsAreStored() {
		err := errors.New(errNoS3BackendsStored)
		traces.SetAndRecordError(span, err)

		return managed.ExternalCreation{}, err
	}

	// allBackendNames is a list of the names of all backends in the backend store.
	allBackendNames := c.backendStore.GetAllBackendNames()

	// backendsToCreateOnNames is a list of names of all backends on which this S3 bucket
	// is to be created. This will either be:
	// 1. The list of bucket.Spec.Providers, if specified.
	// 2. Otherwise, the allBackendNames list.
	// In either case, the list will exclude any backends which have been specified as
	// disabled on the Bucket CR. A backend is specified as disabled for a given bucket
	// if it has been given the backend label (eg 'provider-ceph.backends.<backend-name>: "false"').
	// This means that Provider Ceph will NOT create the bucket on this backend.
	backendsToCreateOnNames := getBucketProvidersFilterDisabledLabel(bucket, allBackendNames)
	if len(backendsToCreateOnNames) == 0 {
		err := errors.New(errAllS3BackendsDisabled)
		traces.SetAndRecordError(span, err)

		return managed.ExternalCreation{}, err
	}

	// Quick sanity check to see if there are backends that the bucket will not be created
	// on and log these backends.
	if len(allBackendNames) != len(backendsToCreateOnNames) {
		log.Info("Bucket will not be created on the following S3 backends", consts.KeyBucketName, bucket.Name, "backends", utils.MissingStrings(allBackendNames, backendsToCreateOnNames))
	}

	// This value shows a bucket on the given backend is already created.
	// It is used to prevent go routines from sending duplicated messages to `readyChan`.
	bucketAlreadyCreated := atomic.Bool{}
	backendCount := 0
	errChan := make(chan error, len(backendsToCreateOnNames))
	readyChan := make(chan string)

	// Now we're ready to start creating S3 buckets on our desired backends.
	for _, beName := range backendsToCreateOnNames {
		originalBucket := bucket.DeepCopy()

		// Attempt to get an S3 client for the backend. This will either be the default
		// S3 client created for each backend by the backend monitor or it will be a new
		// temporary S3 client created via the STS AssumeRole endpoint. The latter will
		// be used if the user has specified an "assume-role-arn" at start-up. If an error
		// occurs, continue to the next backend.
		cl, err := c.s3ClientHandler.GetS3Client(ctx, bucket, beName)
		if err != nil {
			traces.SetAndRecordError(span, err)
			log.Info("Failed to get client for backend - bucket cannot be created on backend", consts.KeyBucketName, originalBucket.Name, consts.KeyBackendName, beName, "error", err.Error())

			continue
		}
		// Increment the backend counter. We need this later to know when the operation should finish.
		backendCount++

		log.Info("Creating bucket on backend", consts.KeyBucketName, originalBucket.Name, consts.KeyBackendName, beName)
		// Launch a go routine for each backend, creating buckets concurrently.
		go func() {
			_, err := rgw.CreateBucket(ctx, cl, rgw.BucketToCreateBucketInput(originalBucket))
			if err != nil {
				log.Info("Failed to create bucket on backend", consts.KeyBucketName, originalBucket.Name, consts.KeyBackendName, beName, "err", err.Error())
				traces.SetAndRecordError(span, err)

				errChan <- err

				return
			}
			log.Info("Bucket created on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)

			// This compare-and-swap operation is the atomic equivalent of:
			//	if *bucketAlreadyCreated == false {
			//		*bucketAlreadyCreated = true
			//		return true
			//	}
			//	return false
			if !bucketAlreadyCreated.CompareAndSwap(false, true) {
				log.Info("Bucket already created on backend - terminate thread without error", "bucket_name", originalBucket.Name, "backend_name", beName)

				errChan <- nil

				return
			}

			// Once a bucket is created successfully on ANY backend, the bucket is considered ready.
			// Therefore we send the name of the backend on which the bucket is first created to the ready channel.
			readyChan <- beName
			errChan <- nil
		}()
	}

	// We couldn't attempt to create a bucket on any backend. We update the bucket CR
	// with the relevant labels and return no error as we do not wish to requeue this
	// Bucket CR while there are no backends for us to create on.
	if backendCount == 0 {
		log.Info("Failed to find any backend for bucket", consts.KeyBucketName, bucket.Name)
		if err := c.updateBucketCR(ctx, bucket, func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
			// Although no backends were found for the bucket, we still apply the backend
			// label to the Bucket CR for each backend that the bucket was intended to be
			// created on. This is to ensure the bucket will eventually be created on these
			// backends.
			setAllBackendLabels(bucketLatest, backendsToCreateOnNames)
			// Pause the Bucket CR because there is no backend for it to be created on.
			// If a backend for which it was intended becomes healthy, the health-check
			// controller will un-pause the Bucket CR (identifying it by its backend label)
			// and it will be re-reconciled.
			bucketLatest.Labels[meta.AnnotationKeyReconciliationPaused] = True

			return NeedsObjectUpdate
		}); err != nil {
			log.Info("Failed to update backend labels", consts.KeyBucketName, bucket.Name)
			err = errors.Wrap(err, errUpdateBucketCR)
			traces.SetAndRecordError(span, err)

			return managed.ExternalCreation{}, err
		}

		return managed.ExternalCreation{}, nil
	}

	return c.waitForCreationAndUpdateBucketCR(ctx, bucket, backendsToCreateOnNames, readyChan, errChan, backendCount)
}

func (c *external) waitForCreationAndUpdateBucketCR(ctx context.Context, bucket *v1alpha1.Bucket, backendsToCreateOnNames []string, readyChan <-chan string, errChan <-chan error, backendCount int) (managed.ExternalCreation, error) {
	ctx, span := otel.Tracer("").Start(ctx, "waitForCreationAndUpdateBucketCR")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, c.log)

	var createErr error

	for i := 0; i < backendCount; i++ {
		select {
		case <-ctx.Done():
			// The create bucket request timed out. Update createErr value, if this is the last error to
			// occur for all our backends then it will be the error that is seen in the Bucket CR Status.
			log.Info("Context timeout waiting for bucket creation", consts.KeyBucketName, bucket.Name)
			createErr = ctx.Err()
		case beName := <-readyChan:
			// This channel receives the value of the backend name that the bucket is first created on.
			// We only need the bucket to be created on a single backend for the Bucket CR to be
			// considered in the Ready condition. Therefore we update the Bucket CR with:
			// 1. The backend labels.
			// 2. The Bucket CR Status with the Ready condition.
			// 3. The Bucket CR Status Backends with a Ready condition for the backend the bucket
			// was created on.
			err := c.updateBucketCR(ctx, bucket, func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
				setAllBackendLabels(bucketLatest, backendsToCreateOnNames)

				return NeedsObjectUpdate
			}, func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
				bucketLatest.Status.SetConditions(xpv1.Available())
				bucketLatest.Status.AtProvider.Backends = v1alpha1.Backends{
					beName: &v1alpha1.BackendInfo{
						BucketCondition: xpv1.Available(),
					},
				}

				return NeedsStatusUpdate
			})
			if err != nil {
				log.Info("Failed to update Bucket CR with backend info", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName, "err", err.Error())

				err := errors.Wrap(err, errUpdateBucketCR)
				traces.SetAndRecordError(span, err)
			}

			return managed.ExternalCreation{}, err
		case createErr = <-errChan:
			// If this channel receives an error it means bucket creation failed on a backend.
			// Therefore, we simply log and continue to the next backend as we only need one
			// successful creation.
			if createErr != nil {
				traces.SetAndRecordError(span, createErr)
			}

			continue
		}
	}

	log.Info("Failed to create bucket on any backend", consts.KeyBucketName, bucket.Name)
	// Update the Bucket CR Status condition to Unavailable. This means the Bucket CR will
	// not be seen as Ready. If that update is successful, we return the createErr which will
	// be the most recent error receieved from a backend's failed creation.
	if err := c.updateBucketCR(ctx, bucket, func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
		bucketLatest.Status.SetConditions(xpv1.Unavailable())

		return NeedsStatusUpdate
	}); err != nil {
		log.Info("Failed to update backend unavailable status on Bucket CR", consts.KeyBucketName, bucket.Name)
		err = errors.Wrap(err, errUpdateBucketCR)
		traces.SetAndRecordError(span, err)

		return managed.ExternalCreation{}, err
	}

	return managed.ExternalCreation{}, createErr
}
