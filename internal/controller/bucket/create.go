package bucket

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/apimachinery/pkg/types"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"
	"github.com/linode/provider-ceph/internal/utils"
)

//nolint:maintidx,gocognit,gocyclo,cyclop,nolintlint // Function requires numerous checks.
func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.external.Create")
	defer span.End()

	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		err := errors.New(errNotBucket)
		traces.SetAndRecordError(span, err)

		return managed.ExternalCreation{}, err
	}

	// Just before Crossplane's over-arching Reconcile function calls external.Create, it
	// updates the MR with the create-pending annotation. After external.Create is finished,
	// the create-success or create-failure annotaion is then added to the MR by the Reconciler
	// depending on the result of external.Create (this function).
	// In the event that an "incomplete creation" occurs (ie the create-pending annotaion is the
	// most recent annotation to be applied out of create-pending, create-failed and create-succeeded)
	// then the MR will not be re-queued. This is to protect against the possibility of "leaked
	// resources" (ie resources which have been created by the provider but are no longer under
	// the provider's control). This must all happen sequentially in a single Reconcile loop.
	// See this Github issue and comment for more context on what is a known issue:
	// https://github.com/crossplane/crossplane/issues/3037#issuecomment-1110142427
	//
	// This approach leaves the possibility for the following scenario:
	// 1. The create-pending annotaion is applied to an MR by Crossplane and external.Create is called.
	// 2. The provider pod crashes before it gets a chance to perform its external.Create function.
	// 3. As a consequence, the resulting create-succeeded or create-failed annotations are never added to
	// the MR.
	// 4. The provider pod comes back online and the MR is automatically reconciled by Crossplane.
	// 5. Crossplane interprets the MR annotaions and believes that an incomplete creation has
	// occurred and therefore does not perform any action on the MR.
	// 6. The MR is effectively paused, awaiting human intervention.
	//
	// To counteract this, we attempt to remove the create-pending annotation as immediately after it
	// has been applied by Crossplane. It is safe for us to do this because the resources we are creating
	// (S3 buckets) have deterministic names. This function will only create a bucket of the given
	// name and will do so idempotently, therefore we are never in danger of creating a cascading
	// number of buckets due to previous failures.
	// Of course, this approach does not completely remove the possibility of us finding ourselves in
	// the above scenario. It only mitigates it. As long as Crossplane persists with its existing logic
	// then we can only make a "best-effort" to avoid it.
	if err := c.updateBucketCR(ctx, bucket, func(_, bucketLatest *v1alpha1.Bucket) UpdateRequired {
		meta.RemoveAnnotations(bucket, meta.AnnotationKeyExternalCreatePending)

		return NeedsObjectUpdate
	}); err != nil {
		c.log.Info("Failed to remove pending annotation", consts.KeyBucketName, bucket.Name)
		err = errors.Wrap(err, errUpdateBucketCR)
		traces.SetAndRecordError(span, err)

		return managed.ExternalCreation{}, err
	}

	span.SetAttributes(attribute.String(consts.KeyBucketName, bucket.Name))

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	if bucket.Spec.Disabled {
		c.log.Info("Bucket is disabled - no buckets to be created on backends", consts.KeyBucketName, bucket.Name)

		return managed.ExternalCreation{}, nil
	}

	if !c.backendStore.BackendsAreStored() {
		err := errors.New(errNoS3BackendsStored)
		traces.SetAndRecordError(span, err)

		return managed.ExternalCreation{}, err
	}

	allBackendNames := c.backendStore.GetAllBackendNames(false)
	providerNames := getBucketProvidersFilterDisabledLabel(bucket, allBackendNames)

	// Create the bucket on each backend in a separate go routine
	activeBackends := c.backendStore.GetActiveBackends(providerNames)
	if len(activeBackends) == 0 {
		err := errors.New(errNoActiveS3Backends)
		traces.SetAndRecordError(span, err)

		return managed.ExternalCreation{}, err
	} else if len(activeBackends) != len(providerNames) {
		c.log.Info("Missing S3 backends", consts.KeyBucketName, bucket.Name, "missing", utils.MissingStrings(providerNames, allBackendNames))
		traces.SetAndRecordError(span, errors.New(errMissingS3Backend))
	}

	// This value shows a bucket on one backend is already created.
	// It is used to prevent goroutines from sending duplicated messages to `readyChan`.
	bucketAlreadyCreated := atomic.Bool{}
	backendCount := 0
	errChan := make(chan error, len(activeBackends))
	readyChan := make(chan string)

	for beName := range activeBackends {
		originalBucket := bucket.DeepCopy()

		cl, err := c.s3ClientHandler.GetS3Client(ctx, bucket, beName)
		if err != nil {
			traces.SetAndRecordError(span, err)
			c.log.Info("Failed to get client for backend - bucket cannot be created on backend", consts.KeyBucketName, originalBucket.Name, consts.KeyBackendName, beName, "error", err.Error())

			continue
		}

		c.log.Info("Creating bucket on backend", consts.KeyBucketName, originalBucket.Name, consts.KeyBackendName, beName)

		pc := &apisv1alpha1.ProviderConfig{}
		if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: beName}, pc); err != nil {
			c.log.Info("Failed to fetch provider config", consts.KeyBucketName, originalBucket.Name, consts.KeyBackendName, beName, "err", err.Error())
			err := errors.Wrap(err, errGetPC)
			traces.SetAndRecordError(span, err)

			return managed.ExternalCreation{}, err
		}

		// Increment the backend counter. We need this later to know when the operation should finish.
		backendCount++

		beName := beName
		go func() {
			_, err := rgw.CreateBucket(ctx, cl, rgw.BucketToCreateBucketInput(originalBucket))
			if err != nil {
				c.log.Info("Failed to create bucket on backend", consts.KeyBucketName, originalBucket.Name, consts.KeyBackendName, beName, "err", err.Error())
				traces.SetAndRecordError(span, err)

				errChan <- err

				return
			}
			c.log.Info("Bucket created on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName)

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
		c.log.Info("Failed to find any backend for bucket", consts.KeyBucketName, bucket.Name)

		if err := c.updateBucketCR(ctx, bucket, func(_, bucketLatest *v1alpha1.Bucket) UpdateRequired {
			// Although no backends were found for the bucket, we still apply the backend
			// label to the Bucket CR for each backend that the bucket was intended to be
			// created on. This is to ensure the bucket will eventually be created on these
			// backends whenever they become active again.
			setAllBackendLabels(bucketLatest, providerNames)

			bucketLatest.Labels[meta.AnnotationKeyReconciliationPaused] = True

			return NeedsObjectUpdate
		}); err != nil {
			c.log.Info("Failed to update backend labels", consts.KeyBucketName, bucket.Name)
			err = errors.Wrap(err, errUpdateBucketCR)
			traces.SetAndRecordError(span, err)

			return managed.ExternalCreation{}, err
		}

		return managed.ExternalCreation{}, nil
	}

	return c.waitForCreationAndUpdateBucketCR(ctx, bucket, providerNames, readyChan, errChan, backendCount)
}

func (c *external) waitForCreationAndUpdateBucketCR(ctx context.Context, bucket *v1alpha1.Bucket, providerNames []string, readyChan <-chan string, errChan <-chan error, backendCount int) (managed.ExternalCreation, error) {
	ctx, span := otel.Tracer("").Start(ctx, "waitForCreationAndUpdateBucketCR")
	defer span.End()

	var createErr error

	for i := 0; i < backendCount; i++ {
		select {
		case <-ctx.Done():
			c.log.Info("Context timeout waiting for bucket creation", consts.KeyBucketName, bucket.Name)
			createErr = ctx.Err()
		case beName := <-readyChan:
			err := c.updateBucketCR(ctx, bucket, func(_, bucketLatest *v1alpha1.Bucket) UpdateRequired {
				setAllBackendLabels(bucketLatest, providerNames)

				return NeedsObjectUpdate
			}, func(_, bucketLatest *v1alpha1.Bucket) UpdateRequired {
				bucketLatest.Status.SetConditions(xpv1.Available())
				bucketLatest.Status.AtProvider.Backends = v1alpha1.Backends{
					beName: &v1alpha1.BackendInfo{
						BucketCondition: xpv1.Available(),
					},
				}

				return NeedsStatusUpdate
			})
			if err != nil {
				c.log.Info("Failed to update Bucket CR with backend info", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, beName, "err", err.Error())

				err := errors.Wrap(err, errUpdateBucketCR)
				traces.SetAndRecordError(span, err)
			}

			return managed.ExternalCreation{}, err
		case createErr = <-errChan:
			if createErr != nil {
				traces.SetAndRecordError(span, createErr)
			}

			continue
		}
	}

	c.log.Info("Failed to create bucket on any backend", consts.KeyBucketName, bucket.Name)

	if err := c.updateBucketCR(ctx, bucket, func(_, bucketLatest *v1alpha1.Bucket) UpdateRequired {
		bucketLatest.Status.SetConditions(xpv1.Unavailable())

		return NeedsStatusUpdate
	}); err != nil {
		c.log.Info("Failed to update backend unavailable status on Bucket CR", consts.KeyBucketName, bucket.Name)
		err = errors.Wrap(err, errUpdateBucketCR)
		traces.SetAndRecordError(span, err)

		return managed.ExternalCreation{}, err
	}

	return managed.ExternalCreation{}, createErr
}
