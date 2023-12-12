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
	ceph "github.com/linode/provider-ceph/internal/ceph"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/clienthandler"
	"github.com/linode/provider-ceph/internal/otel/traces"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"go.opentelemetry.io/otel"
)

// LifecycleConfigurationClient is the client for API methods and reconciling the LifecycleConfiguration
type LifecycleConfigurationClient struct {
	backendStore  *backendstore.BackendStore
	clientHandler *clienthandler.S3ClientHandler
	log           logging.Logger
}

// NewLifecycleConfigurationClient creates the client for Accelerate Configuration
func NewLifecycleConfigurationClient(b *backendstore.BackendStore, c *clienthandler.S3ClientHandler, l logging.Logger) *LifecycleConfigurationClient {
	return &LifecycleConfigurationClient{backendStore: b, clientHandler: c, log: l}
}

func (l *LifecycleConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.LifecycleConfigurationClient.Observe")
	defer span.End()

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
			l.log.Info("Context timeout during bucket lifecycle configuration observation", consts.KeyBucketName, bucket.Name)
			err := errors.Wrap(ctx.Err(), errObserveLifecycleConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		case observation := <-observationChan:
			if observation != Updated {
				return observation, nil
			}
		case err := <-errChan:
			err = errors.Wrap(err, errObserveLifecycleConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		}
	}

	return Updated, nil
}

func (l *LifecycleConfigurationClient) observeBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	l.log.Info("Observing subresource lifecycle configuration on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	if l.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		// If a backend is marked as unhealthy, we can ignore it for now by returning Updated.
		// The backend may be down for some time and we do not want to block Create/Update/Delete
		// calls on other backends. By returning NeedsUpdate here, we would never pass the Observe
		// phase until the backend becomes Healthy or Disabled.
		return Updated, nil
	}

	s3Client := l.backendStore.GetBackendS3Client(backendName)
	response, err := ceph.GetBucketLifecycleConfiguration(ctx, s3Client, aws.String(bucket.Name))
	if err != nil {
		return NeedsUpdate, err
	}

	if bucket.Spec.ForProvider.LifecycleConfiguration == nil || bucket.Spec.LifecycleConfigurationDisabled {
		// No lifecycle config is specified, or it has been disabled.
		// Either way, it should not exist on any backend.
		if response == nil || len(response.Rules) == 0 {
			// No lifecycle config found on this backend.
			l.log.Info("no lifecycle configuration found on backend - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return Updated, nil
		} else {
			l.log.Info("lifecycle configuration found on backend - requires deletion", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return NeedsDeletion, nil
		}
	}

	var local []v1alpha1.LifecycleRule
	if bucket.Spec.ForProvider.LifecycleConfiguration != nil {
		local = bucket.Spec.ForProvider.LifecycleConfiguration.Rules
	}

	var external []s3types.LifecycleRule
	if response != nil {
		external = response.Rules
	}

	ceph.SortFilterTags(external)

	if len(external) != 0 && len(local) == 0 {
		return NeedsDeletion, nil
	}
	// From https://github.com/crossplane-contrib/provider-aws/pkg/controller/s3/bucket/lifecycleConfig.go
	// NOTE(muvaf): We ignore ID because it might have been auto-assigned by AWS
	// and we don't have late-init for this subresource. Besides, a change in ID
	// is almost never expected.
	if !cmp.Equal(external, ceph.GenerateLifecycleRules(local),
		cmpopts.IgnoreFields(s3types.LifecycleRule{}, "ID"), cmpopts.IgnoreTypes(document.NoSerde{})) {
		l.log.Info("lifecycle configuration requires update on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

func (l *LifecycleConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.LifecycleConfigurationClient.Handle")
	defer span.End()

	observation, err := l.observeBackend(ctx, b, backendName)
	if err != nil {
		err = errors.Wrap(err, errHandleLifecycleConfig)
		traces.SetAndRecordError(span, err)

		return err
	}

	switch observation {
	case Updated:
		return nil
	case NeedsDeletion:
		if err := l.delete(ctx, b.Name, backendName); err != nil {
			err = errors.Wrap(err, errHandleLifecycleConfig)
			deleting := xpv1.Deleting().WithMessage(err.Error())
			bb.setLifecycleConfigCondition(b.Name, backendName, &deleting)

			traces.SetAndRecordError(span, err)

			return err
		}
		bb.setLifecycleConfigCondition(b.Name, backendName, nil)

	case NeedsUpdate:
		if err := l.createOrUpdate(ctx, b, backendName); err != nil {
			err = errors.Wrap(err, errHandleLifecycleConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setLifecycleConfigCondition(b.Name, backendName, &unavailable)

			traces.SetAndRecordError(span, err)

			return err
		}
		available := xpv1.Available()
		bb.setLifecycleConfigCondition(b.Name, backendName, &available)
	}

	return nil
}

func (l *LifecycleConfigurationClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	l.log.Info("Updating lifecycle configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := l.clientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = ceph.PutBucketLifecycleConfiguration(ctx, s3Client, b)
	if err != nil {
		return err
	}

	return nil
}

func (l *LifecycleConfigurationClient) delete(ctx context.Context, bucketName, backendName string) error {
	l.log.Info("Deleting lifecycle configuration", consts.KeyBucketName, bucketName, consts.KeyBackendName, backendName)
	s3Client := l.backendStore.GetBackendS3Client(backendName)

	if err := ceph.DeleteBucketLifecycle(ctx, s3Client, aws.String(bucketName)); err != nil {
		return err
	}

	return nil
}
