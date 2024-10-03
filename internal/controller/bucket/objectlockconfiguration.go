package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/document"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"

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

// ObjectLockConfigurationClient is the client for API methods and reconciling the ObjectLockConfiguration
type ObjectLockConfigurationClient struct {
	backendStore    *backendstore.BackendStore
	s3ClientHandler *s3clienthandler.Handler
	log             logging.Logger
}

func NewObjectLockConfigurationClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logging.Logger) *ObjectLockConfigurationClient {
	return &ObjectLockConfigurationClient{backendStore: b, s3ClientHandler: h, log: l}
}

func (l *ObjectLockConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.ObjectLockConfigurationClient.Observe")
	defer span.End()

	if bucket.Spec.ForProvider.ObjectLockEnabledForBucket == nil || !*bucket.Spec.ForProvider.ObjectLockEnabledForBucket {
		l.log.Debug("Object lock configuration not enabled in Bucket CR", consts.KeyBucketName, bucket.Name)

		return Updated, nil
	}

	observationChan := make(chan ResourceStatus)
	errChan := make(chan error)

	for _, backendName := range backendNames {
		beName := backendName
		go func() {
			observation, err := l.observeBackend(ctx, bucket, beName)
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
			l.log.Info("Context timeout during object lock configuration observation", consts.KeyBucketName, bucket.Name)
			err := errors.Wrap(ctx.Err(), errObserveObjectLockConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		case observation := <-observationChan:
			if observation == NeedsUpdate || observation == NeedsDeletion {
				return observation, nil
			}
		case err := <-errChan:
			err = errors.Wrap(err, errObserveObjectLockConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		}
	}

	return Updated, nil
}

func (l *ObjectLockConfigurationClient) observeBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	l.log.Debug("Observing subresource object lock configuration on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	if l.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		// If a backend is marked as unhealthy, we can ignore it for now by returning NoAction.
		// The backend may be down for some time and we do not want to block Create/Update/Delete
		// calls on other backends. By returning NeedsUpdate here, we would never pass the Observe
		// phase until the backend becomes Healthy or Disabled.
		return NoAction, nil
	}

	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
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

	desiredVersioningConfig := rgw.GenerateObjectLockConfiguration(bucket.Spec.ForProvider.ObjectLockConfiguration)

	if !cmp.Equal(external, desiredVersioningConfig, cmpopts.IgnoreTypes(document.NoSerde{})) {
		l.log.Info("Object lock configuration requires update on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

func (l *ObjectLockConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.ObjectLockConfigurationClient.Handle")
	defer span.End()

	if b.Spec.ForProvider.ObjectLockEnabledForBucket == nil || !*b.Spec.ForProvider.ObjectLockEnabledForBucket {
		return nil
	}

	observation, err := l.observeBackend(ctx, b, backendName)
	if err != nil {
		err = errors.Wrap(err, errHandleVersioningConfig)
		traces.SetAndRecordError(span, err)

		return err
	}

	switch observation {
	case NoAction:
		return nil
	case Updated:
		// The object lock config is updated, so we can consider this
		// sub resource Available.
		available := xpv1.Available()
		bb.setObjectLockConfigCondition(b.Name, backendName, &available)

		return nil
	case NeedsDeletion:
		// Object lock configuration, once enabled, cannot be disabled/deleted.
		return nil
	case NeedsUpdate:
		// Object lock configurations cannot be deleted. However, if object lock
		// has been enabled for the bucket and no object lock configuration is
		// specified in the Bucket CR Spec, we should default to a basic "enabled"
		// object lock configuration.
		bucketCopy := b.DeepCopy()
		enabled := v1alpha1.ObjectLockEnabledEnabled
		if b.Spec.ForProvider.ObjectLockConfiguration == nil {
			bucketCopy.Spec.ForProvider.ObjectLockConfiguration = &v1alpha1.ObjectLockConfiguration{
				ObjectLockEnabled: &enabled,
			}
		}
		if err := l.createOrUpdate(ctx, bucketCopy, backendName); err != nil {
			err = errors.Wrap(err, errHandleObjectLockConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setObjectLockConfigCondition(bucketCopy.Name, backendName, &unavailable)

			traces.SetAndRecordError(span, err)

			return err
		}

		available := xpv1.Available()
		bb.setObjectLockConfigCondition(bucketCopy.Name, backendName, &available)
	}

	return nil
}

func (l *ObjectLockConfigurationClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	l.log.Info("Updating object lock configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutObjectLockConfiguration(ctx, s3Client, b)
	if err != nil {
		return err
	}

	return nil
}
