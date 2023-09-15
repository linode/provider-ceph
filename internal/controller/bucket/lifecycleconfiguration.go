package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/document"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	s3internal "github.com/linode/provider-ceph/internal/s3"
	"github.com/pkg/errors"
)

// LifecycleConfigurationClient is the client for API methods and reconciling the LifecycleConfiguration
type LifecycleConfigurationClient struct {
	backendStore *backendstore.BackendStore
	log          logging.Logger
}

// NewLifecycleConfigurationClient creates the client for Accelerate Configuration
func NewLifecycleConfigurationClient(backendStore *backendstore.BackendStore, log logging.Logger) *LifecycleConfigurationClient {
	return &LifecycleConfigurationClient{backendStore: backendStore, log: log}
}

// LifecycleNotFoundErrCode is the error code sent by AWS when the lifecycle config does not exist
var LifecycleNotFoundErrCode = "NoSuchLifecycleConfiguration"

// LifecycleConfigurationNotFound is parses the aws Error and validates if the lifecycle configuration does not exist
func LifecycleConfigurationNotFound(err error) bool {
	var awsErr smithy.APIError
	return errors.As(err, &awsErr) && awsErr.ErrorCode() == LifecycleNotFoundErrCode
}

func (l *LifecycleConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
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
			l.log.Info("Context timeout", "bucket_name", bucket.Name)

			return NeedsUpdate, ctx.Err()

		case observation := <-observationChan:
			if observation != Updated {
				return observation, nil
			}

		case err := <-errChan:

			return NeedsUpdate, err
		}
	}

	return Updated, nil
}

func (l *LifecycleConfigurationClient) observeBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	l.log.Info("Observing subresource lifecycle configuration", "bucket_name", bucket.Name, "backend_name", backendName)

	s3Client := l.backendStore.GetBackendClient(backendName)

	response, err := s3Client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{Bucket: aws.String(bucket.Name)})
	if resource.Ignore(LifecycleConfigurationNotFound, err) != nil {
		return NeedsUpdate, errors.Wrap(err, errGetLifecycleConfig)
	}

	if bucket.Spec.ForProvider.LifecycleConfiguration == nil || bucket.Spec.LifeCycleConfigurationDisabled {
		// No lifecycle config is specified, or it has been disabled.
		// Either way, it should not exist on any backend.
		if LifecycleConfigurationNotFound(err) {
			// No lifecycle config found on this backend.
			return Updated, nil
		} else {
			l.log.Info("lifecycle found on backend - requires deletion", "bucket_name", bucket.Name)

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

	s3internal.SortFilterTags(external)

	if len(external) != 0 && len(local) == 0 {
		return NeedsDeletion, nil
	}
	// From https://github.com/crossplane-contrib/provider-aws/pkg/controller/s3/bucket/lifecycleConfig.go
	// NOTE(muvaf): We ignore ID because it might have been auto-assigned by AWS
	// and we don't have late-init for this subresource. Besides, a change in ID
	// is almost never expected.
	if !cmp.Equal(external, s3internal.GenerateLifecycleRules(local),
		cmpopts.IgnoreFields(s3types.LifecycleRule{}, "ID"), cmpopts.IgnoreTypes(document.NoSerde{})) {
		l.log.Info("lifecycle configuration requires update", "bucket_name", bucket.Name)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

func (l *LifecycleConfigurationClient) HandleObservation(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	observation, err := l.observeBackend(ctx, b, backendName)
	if err != nil {
		return err
	}

	switch observation { //nolint:exhaustive
	case NeedsDeletion:
		bb.setLifecycleConfigStatus(b.Name, backendName, v1alpha1.DeletingStatus)
		err = l.delete(ctx, b.Name, backendName)
		if err != nil {
			return err
		}
		bb.setLifecycleConfigStatus(b.Name, backendName, v1alpha1.NoStatus)

	case NeedsUpdate:
		bb.setLifecycleConfigStatus(b.Name, backendName, v1alpha1.NotReadyStatus)
		if err := l.createOrUpdate(ctx, b, backendName); err != nil {
			return err
		}
		bb.setLifecycleConfigStatus(b.Name, backendName, v1alpha1.ReadyStatus)
	}

	return nil
}

func (l *LifecycleConfigurationClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	l.log.Info("Updating lifecycle configuration", "bucket_name", b.Name, "backend_name", backendName)
	s3Client := l.backendStore.GetBackendClient(backendName)

	_, err := s3Client.PutBucketLifecycleConfiguration(ctx, s3internal.GenerateLifecycleConfigurationInput(b.Name, b.Spec.ForProvider.LifecycleConfiguration))
	if err != nil {
		return errors.Wrap(err, errPutLifecycleConfig)
	}

	return nil
}

func (l *LifecycleConfigurationClient) delete(ctx context.Context, bucketName, backendName string) error {
	l.log.Info("Deleting lifecycle configuration", "bucket_name", bucketName, "backend_name", backendName)
	s3Client := l.backendStore.GetBackendClient(backendName)

	_, err := s3Client.DeleteBucketLifecycle(ctx,
		&s3.DeleteBucketLifecycleInput{
			Bucket: aws.String(bucketName),
		},
	)
	if err != nil {
		return errors.Wrap(err, errDeleteLifecycle)
	}

	return nil
}
