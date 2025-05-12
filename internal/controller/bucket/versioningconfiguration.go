package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/document"
	"github.com/go-logr/logr"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"go.opentelemetry.io/otel"
)

// VersioningConfigurationClient is the client for API methods and reconciling the VersioningConfiguration
type VersioningConfigurationClient struct {
	backendStore    *backendstore.BackendStore
	s3ClientHandler *s3clienthandler.Handler
	log             logr.Logger
}

func NewVersioningConfigurationClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *VersioningConfigurationClient {
	return &VersioningConfigurationClient{backendStore: b, s3ClientHandler: h, log: l}
}

//nolint:dupl // VersioningConfiguration and Policy are different feature.
func (v *VersioningConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.VersioningConfigurationClient.Observe")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, v.log)

	observationChan := make(chan ResourceStatus)
	errChan := make(chan error)

	for _, backendName := range backendNames {
		beName := backendName
		go func() {
			if v.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
				// If a backend is marked as unhealthy, we can ignore it for now by returning NoAction.
				// The backend may be down for some time and we do not want to block Create/Update/Delete
				// calls on other backends. By returning NoAction here, we would never pass the Observe
				// phase until the backend becomes Healthy or Disabled.
				observationChan <- NoAction

				return
			}

			observation, err := v.observeBackend(ctx, bucket, beName)
			if err != nil {
				errChan <- err

				return
			}
			observationChan <- observation
		}()
	}

	for i := 0; i < len(backendNames); i++ {
		select {
		case <-ctx.Done():
			log.Info("Context timeout during bucket versioning configuration observation", consts.KeyBucketName, bucket.Name)
			err := errors.Wrap(ctx.Err(), errObserveVersioningConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		case observation := <-observationChan:
			if observation == NeedsUpdate || observation == NeedsDeletion {
				return observation, nil
			}
		case err := <-errChan:
			err = errors.Wrap(err, errObserveVersioningConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		}
	}

	return Updated, nil
}

func (v *VersioningConfigurationClient) observeBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	ctx, log := traces.InjectTraceAndLogger(ctx, v.log)

	log.V(1).Info("Observing subresource versioning configuration on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	s3Client, err := v.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
	if err != nil {
		return NeedsUpdate, err
	}

	response, err := rgw.GetBucketVersioning(ctx, s3Client, aws.String(bucket.Name))
	if err != nil {
		return NeedsUpdate, err
	}

	if bucket.Spec.ForProvider.VersioningConfiguration == nil &&
		(bucket.Spec.ForProvider.ObjectLockEnabledForBucket == nil || !*bucket.Spec.ForProvider.ObjectLockEnabledForBucket) {
		// No versioining config was defined by the user in the Bucket CR Spec and
		// object lock was not enabled for the bucket. This is should result in
		// (a) an unversioned bucket remaining unversioned OR (b) a versioned bucket
		// having versioning suspended.
		if response == nil || (response.Status == "" && response.MFADelete == "") {
			// An empty versioning configuration was returned from the backend, signifying
			// that versioning was never enabled on this bucket. Therefore versioning is
			// considered Updated for the bucket and we do nothing.
			log.V(1).Info("Versioning is not enabled for bucket on backend - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return NoAction, nil
		} else {
			// A non-empty versioning configuration was returned from the backend, signifying
			// that versioning was previously enabled for this bucket. A bucket cannot be un-versioned,
			// it can only be suspended so we execute this via the NeedsDeletion path.
			log.V(1).Info("Versioning is enabled for bucket on backend - requires suspension", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return NeedsDeletion, nil
		}
	}

	external := &s3types.VersioningConfiguration{}
	if response != nil {
		external.Status = response.Status
		external.MFADelete = s3types.MFADelete(response.MFADelete)
	}

	desiredVersioningConfig := rgw.GenerateVersioningConfiguration(bucket.Spec.ForProvider.VersioningConfiguration)

	if !cmp.Equal(external, desiredVersioningConfig, cmpopts.IgnoreTypes(document.NoSerde{})) {
		log.Info("Versioning configuration requires update on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

func (v *VersioningConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.VersioningConfigurationClient.Handle")
	defer span.End()

	if v.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		traces.SetAndRecordError(span, errUnhealthyBackend)

		return errUnhealthyBackend
	}

	observation, err := v.observeBackend(ctx, b, backendName)
	if err != nil {
		err = errors.Wrap(err, errHandleVersioningConfig)
		traces.SetAndRecordError(span, err)

		return err
	}

	switch observation {
	case NoAction:
		return nil
	case Updated:
		// The versioning config is updated, so we can consider this
		// sub resource Available.
		available := xpv1.Available()
		bb.setVersioningConfigCondition(b.Name, backendName, &available)

		return nil
	case NeedsDeletion:
		// Versioning Configurations are not deleted, only suspended, which requires an update.
		// Create a deep copy of bucket and give it a suspended version config.
		// This will be used in th PutBucketVersioning request to suspend versioning.
		bucketCopy := b.DeepCopy()
		disabled := v1alpha1.MFADeleteDisabled
		suspended := v1alpha1.VersioningStatusSuspended

		bucketCopy.Spec.ForProvider.VersioningConfiguration = &v1alpha1.VersioningConfiguration{
			MFADelete: &disabled,
			Status:    &suspended,
		}
		if err := v.createOrUpdate(ctx, bucketCopy, backendName); err != nil {
			err = errors.Wrap(err, errHandleVersioningConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setVersioningConfigCondition(b.Name, backendName, &unavailable)

			traces.SetAndRecordError(span, err)

			return err
		}
		// Successfully suspended versioning for the backend. Because we cannot
		// un-version a bucket, we must not remove its versioningConfigCondition.
		// Instead, we set it as Available, signifying that the update was a success.
		available := xpv1.Available()
		bb.setVersioningConfigCondition(b.Name, backendName, &available)

		return nil
	case NeedsUpdate:
		bucketCopy := b.DeepCopy()

		// If no versioning configuration was specified, but object lock is enabled
		// for the bucket, then versioning should be enabled without mfa delete.
		// Create a deep copy of bucket and give it an enabled version config.
		// This will be used in th PutBucketVersioning request to enable versioning.
		// If objectLockEnabledForBucket was true upon bucket creation, then this
		// versioning configuration should already exist. But we perform the operation
		// anyway to make sure, as it is idempotent.
		if b.Spec.ForProvider.VersioningConfiguration == nil &&
			b.Spec.ForProvider.ObjectLockEnabledForBucket != nil &&
			*b.Spec.ForProvider.ObjectLockEnabledForBucket {
			enabled := v1alpha1.VersioningStatusEnabled
			disabled := v1alpha1.MFADeleteDisabled

			bucketCopy.Spec.ForProvider.VersioningConfiguration = &v1alpha1.VersioningConfiguration{
				MFADelete: &disabled,
				Status:    &enabled,
			}
		}

		if err := v.createOrUpdate(ctx, bucketCopy, backendName); err != nil {
			err = errors.Wrap(err, errHandleVersioningConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setVersioningConfigCondition(bucketCopy.Name, backendName, &unavailable)

			traces.SetAndRecordError(span, err)

			return err
		}
		available := xpv1.Available()
		bb.setVersioningConfigCondition(bucketCopy.Name, backendName, &available)
	}

	return nil
}

func (v *VersioningConfigurationClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, v.log)

	log.Info("Updating versioniong configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := v.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutBucketVersioning(ctx, s3Client, b)
	if err != nil {
		return err
	}

	return nil
}
