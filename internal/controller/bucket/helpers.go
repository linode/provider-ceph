package bucket

import (
	"context"
	"fmt"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	s3internal "github.com/linode/provider-ceph/internal/s3"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

func setBucketStatus(bucket *v1alpha1.Bucket, bucketBackends *bucketBackends) {
	bucket.Status.SetConditions(xpv1.Unavailable())

	backends := bucketBackends.getBackends(bucket.Name, bucket.Spec.Providers)
	bucket.Status.AtProvider.Backends = backends

	for _, backend := range backends {
		if backend.BucketStatus == v1alpha1.ReadyStatus {
			bucket.Status.SetConditions(xpv1.Available())

			break
		}
	}
}

type UpdateRequired int

const (
	NeedsStatusUpdate UpdateRequired = iota
	NeedsObjectUpdate
)

// Callbacks have two parameters, first bucket is the original, the second is the new version of bucket.
func (c *external) updateObject(ctx context.Context, bucket *v1alpha1.Bucket, callbacks ...func(*v1alpha1.Bucket, *v1alpha1.Bucket) UpdateRequired) error {
	origBucket := bucket.DeepCopy()

	nn := types.NamespacedName{Name: bucket.GetName()}

	const (
		steps  = 3
		divide = 2
		factor = 0.5
		jitter = 0.1
	)

	for _, cb := range callbacks {
		err := retry.OnError(wait.Backoff{
			Steps:    steps,
			Duration: c.operationTimeout / divide,
			Factor:   factor,
			Jitter:   jitter,
		}, resource.IsAPIError, func() error {
			if err := c.kubeClient.Get(ctx, nn, bucket); err != nil {
				return err
			}

			switch cb(origBucket, bucket) {
			case NeedsStatusUpdate:
				return c.kubeClient.Status().Update(ctx, bucket)
			case NeedsObjectUpdate:
				return c.kubeClient.Update(ctx, bucket)
			default:
				return nil
			}
		})

		if err != nil {
			if kerrors.IsNotFound(err) {
				c.log.Info("Bucket doesn't exists", "bucket_name", bucket.Name)

				break
			}

			return errors.Wrap(err, "unable to update object")
		}
	}

	return nil
}

func (c *external) GetClient(ctx context.Context, bucket *v1alpha1.Bucket, beName string) (backendstore.S3Client, error) {
	if bucket.Spec.ForProvider.AssumeRole == nil {
		return c.backendStore.GetBackendS3Client(beName), nil
	}

	arOutput, err := c.backendStore.GetBackendSTSClient(beName).AssumeRole(ctx, s3internal.AssumeRoleInput(bucket.Spec.ForProvider.AssumeRole))
	if err != nil {
		return nil, err
	}

	if arOutput.Credentials == nil {
		return nil, fmt.Errorf("No credentials in AssumeRoleOutput")
	}
	if arOutput.Credentials.AccessKeyId == nil {
		return nil, fmt.Errorf("No access key in AssumeRoleOutput")

	}
	if arOutput.Credentials.SecretAccessKey == nil {
		return nil, fmt.Errorf("No secret access key in AssumeRoleOutput")
	}

	data := map[string][]byte{
		"access_key": []byte(*arOutput.Credentials.AccessKeyId),
		"secret_key": []byte(*arOutput.Credentials.SecretAccessKey)}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: beName}, pc); err != nil {
		return nil, err
	}

	return s3internal.NewS3Client(ctx, data, pc.Spec.HostBase, pc.Spec.UseHTTPS)
}
