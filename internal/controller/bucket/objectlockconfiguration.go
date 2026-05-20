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

// ObjectLockConfigurationClient is the client for API methods and reconciling the ObjectLockConfiguration
type ObjectLockConfigurationClient struct {
	BaseSubresourceClient
}

func NewObjectLockConfigurationClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *ObjectLockConfigurationClient {
	return &ObjectLockConfigurationClient{BaseSubresourceClient: NewBaseSubresourceClient(b, h, l)}
}

func (o *ObjectLockConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	return o.BaseSubresourceClient.Observe(ctx, bucket, backendNames, o)
}

func (o *ObjectLockConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	return o.BaseSubresourceClient.Handle(ctx, b, backendName, bb, o)
}

// Implement Subresource interface

func (o *ObjectLockConfigurationClient) SkipObservation(bucket *v1alpha1.Bucket) bool {
	return bucket.Spec.ForProvider.ObjectLockEnabledForBucket == nil || !*bucket.Spec.ForProvider.ObjectLockEnabledForBucket
}

func (o *ObjectLockConfigurationClient) GetLogger() logr.Logger {
	return o.log
}

func (o *ObjectLockConfigurationClient) GetBackendStore() *backendstore.BackendStore {
	return o.backendStore
}

func (o *ObjectLockConfigurationClient) GetS3ClientHandler() *s3clienthandler.Handler {
	return o.s3ClientHandler
}

func (o *ObjectLockConfigurationClient) GetObserveErrorMsg() string {
	return errObserveObjectLockConfig
}

func (o *ObjectLockConfigurationClient) ObserveBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	ctx, log := traces.InjectTraceAndLogger(ctx, o.log)

	log.V(1).Info("Observing subresource object lock configuration on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	s3Client, err := o.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
	if err != nil {
		return NeedsUpdate, err
	}
	response, err := rgw.GetObjectLockConfiguration(ctx, s3Client, aws.String(bucket.Name))
	if err != nil {
		return NeedsUpdate, err
	}

	external := &s3types.ObjectLockConfiguration{}
	if response != nil && response.ObjectLockConfiguration != nil {
		external = response.ObjectLockConfiguration
	}

	desiredObjectLockConfig := rgw.GenerateObjectLockConfiguration(bucket.Spec.ForProvider.ObjectLockConfiguration)

	if !cmp.Equal(external, desiredObjectLockConfig, cmpopts.IgnoreTypes(document.NoSerde{})) {
		log.Info("Object lock configuration requires update on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

// Implement Subresource interface

func (o *ObjectLockConfigurationClient) GetHandleErrorMsg() string {
	return errHandleObjectLockConfig
}

func (o *ObjectLockConfigurationClient) GetSubresourceName() string {
	return "ObjectLockConfigurationClient"
}

func (o *ObjectLockConfigurationClient) HandleObservation(ctx context.Context, observation ResourceStatus, bucket *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	switch observation {
	case NoAction:
		return nil
	case Updated:
		// The object lock config is updated, so we can consider this
		// sub resource Available.
		available := xpv1.Available()
		bb.setObjectLockConfigCondition(bucket.Name, backendName, &available)
		return nil
	case NeedsDeletion:
		// Object lock configuration, once enabled, cannot be disabled/deleted.
		return nil
	case NeedsUpdate:
		// Object lock configurations cannot be deleted. However, if object lock
		// has been enabled for the bucket and no object lock configuration is
		// specified in the Bucket CR Spec, we should default to a basic "enabled"
		// object lock configuration.
		bucketCopy := bucket.DeepCopy()
		enabled := v1alpha1.ObjectLockEnabledEnabled
		if bucket.Spec.ForProvider.ObjectLockConfiguration == nil {
			bucketCopy.Spec.ForProvider.ObjectLockConfiguration = &v1alpha1.ObjectLockConfiguration{
				ObjectLockEnabled: &enabled,
			}
		}
		if err := o.createOrUpdate(ctx, bucketCopy, backendName); err != nil {
			err = errors.Wrap(err, errHandleObjectLockConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setObjectLockConfigCondition(bucketCopy.Name, backendName, &unavailable)
			return err
		}

		available := xpv1.Available()
		bb.setObjectLockConfigCondition(bucketCopy.Name, backendName, &available)
		return nil
	}
	return nil
}

func (o *ObjectLockConfigurationClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, o.log)

	log.Info("Updating object lock configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := o.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutObjectLockConfiguration(ctx, s3Client, b)
	if err != nil {
		return err
	}

	return nil
}
