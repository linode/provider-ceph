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

// LifecycleConfigurationClient is the client for API methods and reconciling the LifecycleConfiguration
type LifecycleConfigurationClient struct {
	backendStore    *backendstore.BackendStore
	s3ClientHandler *s3clienthandler.Handler
	log             logr.Logger
}

func NewLifecycleConfigurationClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *LifecycleConfigurationClient {
	return &LifecycleConfigurationClient{backendStore: b, s3ClientHandler: h, log: l}
}

//nolint:dupl // LifecycleConfiguration and Policy are different feature.
func (l *LifecycleConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.LifecycleConfigurationClient.Observe")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, l.log)

	observationChan := make(chan ResourceStatus)
	errChan := make(chan error)

	for _, backendName := range backendNames {
		beName := backendName
		go func() {
			if l.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
				// If a backend is marked as unhealthy, we can ignore it for now by returning NoAction.
				// The backend may be down for some time and we do not want to block Create/Update/Delete
				// calls on other backends. By returning NoAction here, we would never pass the Observe
				// phase until the backend becomes Healthy or Disabled.
				observationChan <- NoAction

				return
			}

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
			log.Info("Context timeout during bucket lifecycle configuration observation", consts.KeyBucketName, bucket.Name)
			err := errors.Wrap(ctx.Err(), errObserveLifecycleConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		case observation := <-observationChan:
			if observation == NeedsUpdate || observation == NeedsDeletion {
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
	ctx, log := traces.InjectTraceAndLogger(ctx, l.log)

	log.V(1).Info("Observing subresource lifecycle configuration on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
	if err != nil {
		return NeedsUpdate, err
	}
	response, err := rgw.GetBucketLifecycleConfiguration(ctx, s3Client, aws.String(bucket.Name))
	if err != nil {
		return NeedsUpdate, err
	}

	if bucket.Spec.ForProvider.LifecycleConfiguration == nil || bucket.Spec.LifecycleConfigurationDisabled {
		// No lifecycle config is specified, or it has been disabled.
		// Either way, it should not exist on any backend.
		if response == nil || len(response.Rules) == 0 {
			// No lifecycle config found on this backend.
			log.V(1).Info("No lifecycle configuration found on backend - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return NoAction, nil
		} else {
			log.V(1).Info("Lifecycle configuration found on backend - requires deletion", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

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

	rgw.SortFilterTags(external)

	if len(external) != 0 && len(local) == 0 {
		return NeedsDeletion, nil
	}

	if !cmp.Equal(external, rgw.GenerateLifecycleRules(local), cmpopts.IgnoreTypes(document.NoSerde{})) {
		log.V(1).Info("Lifecycle configuration requires update on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

func (l *LifecycleConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.LifecycleConfigurationClient.Handle")
	defer span.End()

	if l.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		traces.SetAndRecordError(span, errUnhealthyBackend)

		return errUnhealthyBackend
	}

	observation, err := l.observeBackend(ctx, b, backendName)
	if err != nil {
		err = errors.Wrap(err, errHandleLifecycleConfig)
		traces.SetAndRecordError(span, err)

		return err
	}

	switch observation {
	case NoAction:
		return nil
	case Updated:
		// The lifecycle config is updated, so we can consider this
		// sub resource Available.
		available := xpv1.Available()
		bb.setLifecycleConfigCondition(b.Name, backendName, &available)

	case NeedsDeletion:
		if err := l.delete(ctx, b, backendName); err != nil {
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
	ctx, log := traces.InjectTraceAndLogger(ctx, l.log)

	log.Info("Updating lifecycle configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutBucketLifecycleConfiguration(ctx, s3Client, b)
	if err != nil {
		return err
	}

	return nil
}

func (l *LifecycleConfigurationClient) delete(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, l.log)

	log.Info("Deleting lifecycle configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	if err := rgw.DeleteBucketLifecycle(ctx, s3Client, aws.String(b.Name)); err != nil {
		return err
	}

	return nil
}
