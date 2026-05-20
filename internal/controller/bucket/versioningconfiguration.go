package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/document"
	"github.com/go-logr/logr"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// VersioningConfigurationClient is the client for API methods and reconciling the VersioningConfiguration
type VersioningConfigurationClient struct {
	BaseSubresourceClient
}

func NewVersioningConfigurationClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *VersioningConfigurationClient {
	return &VersioningConfigurationClient{BaseSubresourceClient: NewBaseSubresourceClient(b, h, l)}
}

//nolint:dupl // VersioningConfiguration is a different feature.
func (v *VersioningConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	return v.BaseSubresourceClient.Observe(ctx, bucket, backendNames, v)
}

func (v *VersioningConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	return v.BaseSubresourceClient.Handle(ctx, b, backendName, bb, v)
}

// Implement Subresource interface

func (v *VersioningConfigurationClient) GetLogger() logr.Logger {
	return v.log
}

func (v *VersioningConfigurationClient) GetBackendStore() *backendstore.BackendStore {
	return v.backendStore
}

func (v *VersioningConfigurationClient) GetS3ClientHandler() *s3clienthandler.Handler {
	return v.s3ClientHandler
}

func (v *VersioningConfigurationClient) GetObserveErrorMsg() string {
	return errObserveVersioningConfig
}

func (v *VersioningConfigurationClient) ObserveBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
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

// Implement Subresource interface

func (v *VersioningConfigurationClient) GetHandleErrorMsg() string {
	return errHandleVersioningConfig
}

func (v *VersioningConfigurationClient) GetSubresourceName() string {
	return "VersioningConfigurationClient"
}

func (v *VersioningConfigurationClient) HandleObservation(ctx context.Context, observation ResourceStatus, bucket *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	switch observation {
	case NoAction:
		return nil
	case Updated:
		// The versioning config is updated, so we can consider this
		// sub resource Available.
		available := xpv1.Available()
		bb.setVersioningConfigCondition(bucket.Name, backendName, &available)
		return nil
	case NeedsDeletion:
		// Versioning Configurations are not deleted, only suspended, which requires an update.
		// Create a deep copy of bucket and give it a suspended version config.
		// This will be used in th PutBucketVersioning request to suspend versioning.
		bucketCopy := bucket.DeepCopy()
		disabled := v1alpha1.MFADeleteDisabled
		suspended := v1alpha1.VersioningStatusSuspended

		bucketCopy.Spec.ForProvider.VersioningConfiguration = &v1alpha1.VersioningConfiguration{
			MFADelete: &disabled,
			Status:    &suspended,
		}
		if err := v.createOrUpdate(ctx, bucketCopy, backendName); err != nil {
			err = errors.Wrap(err, errHandleVersioningConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setVersioningConfigCondition(bucket.Name, backendName, &unavailable)
			return err
		}
		// Successfully suspended versioning for the backend. Because we cannot
		// un-version a bucket, we must not remove its versioningConfigCondition.
		// Instead, we set it as Available, signifying that the update was a success.
		available := xpv1.Available()
		bb.setVersioningConfigCondition(bucket.Name, backendName, &available)
		return nil
	case NeedsUpdate:
		bucketCopy := bucket.DeepCopy()

		// If no versioning configuration was specified, but object lock is enabled
		// for the bucket, then versioning should be enabled without mfa delete.
		// Create a deep copy of bucket and give it an enabled version config.
		// This will be used in th PutBucketVersioning request to enable versioning.
		// If objectLockEnabledForBucket was true upon bucket creation, then this
		// versioning configuration should already exist. But we perform the operation
		// anyway to make sure, as it is idempotent.
		if bucket.Spec.ForProvider.VersioningConfiguration == nil &&
			bucket.Spec.ForProvider.ObjectLockEnabledForBucket != nil &&
			*bucket.Spec.ForProvider.ObjectLockEnabledForBucket {
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
			return err
		}
		available := xpv1.Available()
		bb.setVersioningConfigCondition(bucketCopy.Name, backendName, &available)
		return nil
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
