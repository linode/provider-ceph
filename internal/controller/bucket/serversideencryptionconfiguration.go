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

// ServerSideEncryptionConfigurationClient is the client for API methods and reconciling the ServerSideEncryptionConfiguration
type ServerSideEncryptionConfigurationClient struct {
	backendStore    *backendstore.BackendStore
	s3ClientHandler *s3clienthandler.Handler
	log             logr.Logger
}

func NewServerSideEncryptionConfigurationClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *ServerSideEncryptionConfigurationClient {
	return &ServerSideEncryptionConfigurationClient{backendStore: b, s3ClientHandler: h, log: l}
}

//nolint:dupl // ServerSideEncryptionConfiguration is similar to other subresource clients.
func (l *ServerSideEncryptionConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.ServerSideEncryptionConfigurationClient.Observe")
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
			log.Info("Context timeout during bucket server side encryption configuration observation", consts.KeyBucketName, bucket.Name)
			err := errors.Wrap(ctx.Err(), errObserveSSEConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		case observation := <-observationChan:
			if observation == NeedsUpdate || observation == NeedsDeletion {
				return observation, nil
			}
		case err := <-errChan:
			err = errors.Wrap(err, errObserveSSEConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		}
	}

	return Updated, nil
}

//nolint:gocyclo,cyclop // Function requires numerous checks.
func (l *ServerSideEncryptionConfigurationClient) observeBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	ctx, log := traces.InjectTraceAndLogger(ctx, l.log)

	log.V(1).Info("Observing subresource server side encryption configuration on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
	if err != nil {
		return NeedsUpdate, err
	}
	response, err := rgw.GetBucketEncryption(ctx, s3Client, aws.String(bucket.Name))
	if err != nil {
		return NeedsUpdate, err
	}

	if bucket.Spec.ForProvider.ServerSideEncryptionConfiguration == nil || bucket.Spec.ServerSideEncryptionConfigurationDisabled {
		// No SSE config is specified, or it has been disabled.
		// Either way, it should not exist on any backend.
		if response == nil ||
			response.ServerSideEncryptionConfiguration == nil ||
			len(response.ServerSideEncryptionConfiguration.Rules) == 0 {
			// No SSE config found on this backend.
			log.V(1).Info("No server side encryption configuration found on backend - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return NoAction, nil
		} else {
			log.V(1).Info("Server side encryption configuration found on backend - requires deletion", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return NeedsDeletion, nil
		}
	}

	var sseRulesInCR []v1alpha1.ServerSideEncryptionRule
	if bucket.Spec.ForProvider.ServerSideEncryptionConfiguration != nil {
		sseRulesInCR = bucket.Spec.ForProvider.ServerSideEncryptionConfiguration.Rules
	}

	var sseRulesOnBackend []s3types.ServerSideEncryptionRule
	if response != nil && response.ServerSideEncryptionConfiguration != nil {
		sseRulesOnBackend = response.ServerSideEncryptionConfiguration.Rules
	}

	if len(sseRulesOnBackend) != 0 && len(sseRulesInCR) == 0 {
		return NeedsDeletion, nil
	}

	if !cmp.Equal(sseRulesOnBackend, rgw.GenerateServerSideEncryptionRules(sseRulesInCR), cmpopts.IgnoreTypes(document.NoSerde{})) {
		log.V(1).Info("ServerSideEncryption configuration requires update on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

//nolint:dupl // ServerSideEncryptionConfiguration and LifecycleConfiguration have similar Handle logic.
func (l *ServerSideEncryptionConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.ServerSideEncryptionConfigurationClient.Handle")
	defer span.End()

	if l.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		traces.SetAndRecordError(span, errUnhealthyBackend)

		return errUnhealthyBackend
	}

	observation, err := l.observeBackend(ctx, b, backendName)
	if err != nil {
		err = errors.Wrap(err, errHandleSSEConfig)
		traces.SetAndRecordError(span, err)

		return err
	}

	switch observation {
	case NoAction:
		return nil
	case Updated:
		// The SSE config is updated, so we can consider this
		// sub resource Available.
		available := xpv1.Available()
		bb.setSSEConfigCondition(b.Name, backendName, &available)

	case NeedsDeletion:
		if err := l.delete(ctx, b, backendName); err != nil {
			err = errors.Wrap(err, errHandleSSEConfig)
			deleting := xpv1.Deleting().WithMessage(err.Error())
			bb.setSSEConfigCondition(b.Name, backendName, &deleting)

			traces.SetAndRecordError(span, err)

			return err
		}
		bb.setSSEConfigCondition(b.Name, backendName, nil)

	case NeedsUpdate:
		if err := l.createOrUpdate(ctx, b, backendName); err != nil {
			err = errors.Wrap(err, errHandleSSEConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setSSEConfigCondition(b.Name, backendName, &unavailable)

			traces.SetAndRecordError(span, err)

			return err
		}
		available := xpv1.Available()
		bb.setSSEConfigCondition(b.Name, backendName, &available)
	}

	return nil
}

func (l *ServerSideEncryptionConfigurationClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, l.log)

	log.Info("Updating server side encryption configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutBucketEncryption(ctx, s3Client, b)
	if err != nil {
		return err
	}

	return nil
}

func (l *ServerSideEncryptionConfigurationClient) delete(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, l.log)

	log.Info("Deleting server side encryption configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	if err := rgw.DeleteBucketEncryption(ctx, s3Client, aws.String(b.Name)); err != nil {
		return err
	}

	return nil
}
