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

// ServerSideEncryptionConfigurationClient is the client for API methods and reconciling the ServerSideEncryptionConfiguration
type ServerSideEncryptionConfigurationClient struct {
	BaseSubresourceClient
}

func NewServerSideEncryptionConfigurationClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *ServerSideEncryptionConfigurationClient {
	return &ServerSideEncryptionConfigurationClient{BaseSubresourceClient: NewBaseSubresourceClient(b, h, l)}
}

func (s *ServerSideEncryptionConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	return s.BaseSubresourceClient.Observe(ctx, bucket, backendNames, s)
}

func (s *ServerSideEncryptionConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	return s.BaseSubresourceClient.Handle(ctx, b, backendName, bb, s)
}

// Implement Subresource interface

func (s *ServerSideEncryptionConfigurationClient) GetLogger() logr.Logger {
	return s.log
}

func (s *ServerSideEncryptionConfigurationClient) GetBackendStore() *backendstore.BackendStore {
	return s.backendStore
}

func (s *ServerSideEncryptionConfigurationClient) GetS3ClientHandler() *s3clienthandler.Handler {
	return s.s3ClientHandler
}

func (s *ServerSideEncryptionConfigurationClient) GetObserveErrorMsg() string {
	return errObserveSSEConfig
}

//nolint:gocyclo,cyclop // Function requires numerous checks.
func (s *ServerSideEncryptionConfigurationClient) ObserveBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	ctx, log := traces.InjectTraceAndLogger(ctx, s.log)

	log.V(1).Info("Observing subresource server side encryption configuration on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	s3Client, err := s.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
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

// Implement Subresource interface

func (s *ServerSideEncryptionConfigurationClient) GetHandleErrorMsg() string {
	return errHandleSSEConfig
}

func (s *ServerSideEncryptionConfigurationClient) GetSubresourceName() string {
	return "ServerSideEncryptionConfigurationClient"
}

//nolint:dupl // Pattern is intentionally shared with other subresources (LifecycleConfiguration)
func (s *ServerSideEncryptionConfigurationClient) HandleObservation(ctx context.Context, observation ResourceStatus, bucket *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	switch observation {
	case NoAction:
		return nil
	case Updated:
		// The SSE config is updated, so we can consider this
		// sub resource Available.
		available := xpv1.Available()
		bb.setSSEConfigCondition(bucket.Name, backendName, &available)

		return nil
	case NeedsDeletion:
		if err := s.delete(ctx, bucket, backendName); err != nil {
			err = errors.Wrap(err, errHandleSSEConfig)
			deleting := xpv1.Deleting().WithMessage(err.Error())
			bb.setSSEConfigCondition(bucket.Name, backendName, &deleting)

			return err
		}
		bb.setSSEConfigCondition(bucket.Name, backendName, nil)

		return nil
	case NeedsUpdate:
		if err := s.createOrUpdate(ctx, bucket, backendName); err != nil {
			err = errors.Wrap(err, errHandleSSEConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setSSEConfigCondition(bucket.Name, backendName, &unavailable)

			return err
		}
		available := xpv1.Available()
		bb.setSSEConfigCondition(bucket.Name, backendName, &available)

		return nil
	}

	return nil
}

func (s *ServerSideEncryptionConfigurationClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, s.log)

	log.Info("Updating server side encryption configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := s.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutBucketEncryption(ctx, s3Client, b)
	if err != nil {
		return err
	}

	return nil
}

func (s *ServerSideEncryptionConfigurationClient) delete(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, s.log)

	log.Info("Deleting server side encryption configuration", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := s.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	if err := rgw.DeleteBucketEncryption(ctx, s3Client, aws.String(b.Name)); err != nil {
		return err
	}

	return nil
}
