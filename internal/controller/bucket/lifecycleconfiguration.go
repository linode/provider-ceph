package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/document"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	s3internal "github.com/linode/provider-ceph/internal/s3"
	"github.com/pkg/errors"
)

// LifecycleNotFoundErrCode is the error code sent by AWS when the lifecycle config does not exist
var LifecycleNotFoundErrCode = "NoSuchLifecycleConfiguration"

// LifecycleConfigurationNotFound is parses the aws Error and validates if the lifecycle configuration does not exist
func LifecycleConfigurationNotFound(err error) bool {
	var awsErr smithy.APIError
	return errors.As(err, &awsErr) && awsErr.ErrorCode() == LifecycleNotFoundErrCode
}

func (c *external) observeLifecycleConfig(ctx context.Context, bucket *v1alpha1.Bucket) (bool, error) {
	s3Clients := c.backendStore.GetBackendClients(bucket.Spec.Providers)

	c.log.Info("Observing subresource: lifecycle configuration", "bucket_name", bucket.Name)
	needsUpdateChan := make(chan bool)
	errChan := make(chan error)

	for _, client := range s3Clients {
		cl := client
		go func() {
			needsUpdate, err := c.lcNeedsUpdate(ctx, bucket, cl)
			if err != nil {
				errChan <- err
			}

			needsUpdateChan <- needsUpdate
		}()
	}

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Context timeout", "bucket_name", bucket.Name)

			return false, ctx.Err()
		case needsUpdate := <-needsUpdateChan:

			return needsUpdate, nil
		case err := <-errChan:

			return false, err
		}
	}
}

func (c *external) lcNeedsUpdate(ctx context.Context, bucket *v1alpha1.Bucket, s3Client backendstore.S3Client) (bool, error) {
	response, err := s3Client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{Bucket: aws.String(bucket.Name)})
	if (bucket.Spec.ForProvider.LifecycleConfiguration == nil || bucket.Spec.LifeCycleConfigurationDisabled) && LifecycleConfigurationNotFound(err) {
		return false, nil
	}

	if bucket.Spec.LifeCycleConfigurationDisabled == true && !LifecycleConfigurationNotFound(err) {
		c.log.Info("lifecycle configuration has been disabled, but exists on one or more backends - requeue for update", "bucket_name", bucket.Name)

		return true, nil
	}

	if resource.Ignore(LifecycleConfigurationNotFound, err) != nil {
		return false, errors.Wrap(err, errGetLifecycleConfig)
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

	// NOTE(muvaf): We ignore ID because it might have been auto-assigned by AWS
	// and we don't have late-init for this subresource. Besides, a change in ID
	// is almost never expected.
	if !cmp.Equal(external, s3internal.GenerateLifecycleRules(local),
		cmpopts.IgnoreFields(s3types.LifecycleRule{}, "ID"), cmpopts.IgnoreTypes(document.NoSerde{})) {
		c.log.Info("lifecycle configuration requires update", "bucket_name", bucket.Name)

		return true, nil
	}

	return false, nil
}

func (c *external) updateLifecycleConfig(ctx context.Context, backendName string, b *v1alpha1.Bucket, cl backendstore.S3Client, bb *bucketBackends) error {

	bb.setLifecycleConfigStatus(b.Name, backendName, v1alpha1.NotReadyStatus)
	_, err := cl.PutBucketLifecycleConfiguration(ctx, s3internal.GenerateLifecycleConfigurationInput(b.Name, b.Spec.ForProvider.LifecycleConfiguration))
	if err != nil {
		return errors.Wrap(err, errPutLifecycleConfig)
	}
	bb.setLifecycleConfigStatus(b.Name, backendName, v1alpha1.ReadyStatus)

	return nil
}

func (c *external) deleteLifecycleConfig(ctx context.Context, backendName string, b *v1alpha1.Bucket, cl backendstore.S3Client, bb *bucketBackends) error {
	bb.setLifecycleConfigStatus(b.Name, backendName, v1alpha1.DeletingStatus)

	_, err := cl.DeleteBucketLifecycle(ctx,
		&s3.DeleteBucketLifecycleInput{
			Bucket: aws.String(b.Name),
		},
	)
	if err != nil {
		return errors.Wrap(err, errDeleteLifecycle)
	}

	bb.setLifecycleConfigStatus(b.Name, backendName, v1alpha1.NoStatus)

	return nil
}
