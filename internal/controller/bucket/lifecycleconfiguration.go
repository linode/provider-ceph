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

// LifecycleConfigurationClient is the client for API methods and reconciling the LifecycleConfiguration
type LifecycleConfigurationClient struct {
	BaseSubresourceClient
}

func NewLifecycleConfigurationClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *LifecycleConfigurationClient {
	return &LifecycleConfigurationClient{BaseSubresourceClient: NewBaseSubresourceClient(b, h, l)}
}

//nolint:dupl // LifecycleConfiguration is a different feature.
func (l *LifecycleConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	return l.BaseSubresourceClient.Observe(ctx, bucket, backendNames, l)
}

func (l *LifecycleConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	return l.BaseSubresourceClient.Handle(ctx, b, backendName, bb, l)
}

// Implement Subresource interface

func (l *LifecycleConfigurationClient) GetLogger() logr.Logger {
	return l.log
}

func (l *LifecycleConfigurationClient) GetBackendStore() *backendstore.BackendStore {
	return l.backendStore
}

func (l *LifecycleConfigurationClient) GetS3ClientHandler() *s3clienthandler.Handler {
	return l.s3ClientHandler
}

func (l *LifecycleConfigurationClient) GetObserveErrorMsg() string {
	return errObserveLifecycleConfig
}

func (l *LifecycleConfigurationClient) ObserveBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
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

// Implement Subresource interface

func (l *LifecycleConfigurationClient) GetHandleErrorMsg() string {
	return errHandleLifecycleConfig
}

func (l *LifecycleConfigurationClient) GetSubresourceName() string {
	return "LifecycleConfigurationClient"
}

func (l *LifecycleConfigurationClient) HandleObservation(ctx context.Context, observation ResourceStatus, bucket *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	switch observation {
	case NoAction:
		return nil
	case Updated:
		// The lifecycle config is updated, so we can consider this
		// sub resource Available.
		available := xpv1.Available()
		bb.setLifecycleConfigCondition(bucket.Name, backendName, &available)
		return nil
	case NeedsDeletion:
		if err := l.delete(ctx, bucket, backendName); err != nil {
			err = errors.Wrap(err, errHandleLifecycleConfig)
			deleting := xpv1.Deleting().WithMessage(err.Error())
			bb.setLifecycleConfigCondition(bucket.Name, backendName, &deleting)
			return err
		}
		bb.setLifecycleConfigCondition(bucket.Name, backendName, nil)
		return nil
	case NeedsUpdate:
		if err := l.createOrUpdate(ctx, bucket, backendName); err != nil {
			err = errors.Wrap(err, errHandleLifecycleConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setLifecycleConfigCondition(bucket.Name, backendName, &unavailable)
			return err
		}
		available := xpv1.Available()
		bb.setLifecycleConfigCondition(bucket.Name, backendName, &available)
		return nil
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
