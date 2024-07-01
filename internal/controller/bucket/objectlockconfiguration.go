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

//nolint:dupl // All subresources require similar Observe method.
func (l *ObjectLockConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.ObjectLockConfigurationClient.Observe")
	defer span.End()

	if bucket.Spec.ForProvider.ObjectLockConfiguration == nil {
		l.log.Info("No object lock configuration specified in Bucket CR - object lock cannot be disabled so no action required", consts.KeyBucketName, bucket.Name)

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
			if observation != Updated {
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
	l.log.Info("Observing subresource object lock configuration on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	if l.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		// If a backend is marked as unhealthy, we can ignore it for now by returning Updated.
		// The backend may be down for some time and we do not want to block Create/Update/Delete
		// calls on other backends. By returning NeedsUpdate here, we would never pass the Observe
		// phase until the backend becomes Healthy or Disabled.
		return Updated, nil
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
	if response.ObjectLockConfiguration != nil {
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

	if b.Spec.ForProvider.ObjectLockConfiguration == nil {
		l.log.Info("No object lock configuration specified in Bucket CR - object lock cannot be disabled so no action required on backend", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)

		return nil
	}

	observation, err := l.observeBackend(ctx, b, backendName)
	if err != nil {
		err = errors.Wrap(err, errHandleVersioningConfig)
		traces.SetAndRecordError(span, err)

		return err
	}

	switch observation {
	case Updated:
		return nil
	case NeedsDeletion:
		// Object lock configuration, once enabled, cannot be disabled/deleted.
		return nil
	case NeedsUpdate:
		if err := l.createOrUpdate(ctx, b, backendName); err != nil {
			err = errors.Wrap(err, errHandleObjectLockConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setObjectLockConfigCondition(b.Name, backendName, &unavailable)

			traces.SetAndRecordError(span, err)

			return err
		}
		available := xpv1.Available()
		bb.setObjectLockConfigCondition(b.Name, backendName, &available)
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
