package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-logr/logr"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"
)

// ACLClient is the client for API methods and reconciling the ACL
type ACLClient struct {
	BaseSubresourceClient
}

// NewACLClient creates the client for ACL
func NewACLClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *ACLClient {
	return &ACLClient{BaseSubresourceClient: NewBaseSubresourceClient(b, h, l)}
}

func (a *ACLClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	return a.BaseSubresourceClient.Observe(ctx, bucket, backendNames, a)
}

func (a *ACLClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	return a.BaseSubresourceClient.Handle(ctx, b, backendName, bb, a)
}

// Implement Subresource interface

func (a *ACLClient) GetLogger() logr.Logger {
	return a.log
}

func (a *ACLClient) GetBackendStore() *backendstore.BackendStore {
	return a.backendStore
}

func (a *ACLClient) GetS3ClientHandler() *s3clienthandler.Handler {
	return a.s3ClientHandler
}

func (a *ACLClient) GetObserveErrorMsg() string {
	return errObserveAcl
}

func (a *ACLClient) ObserveBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	_, log := traces.InjectTraceAndLogger(ctx, a.log)

	log.V(1).Info("Observing subresource acl on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	// If your bucket uses the bucket owner enforced setting for S3 Object
	// Ownership, ACLs are disabled and no longer affect permissions.
	if s3types.ObjectOwnership(aws.ToString(bucket.Spec.ForProvider.ObjectOwnership)) == s3types.ObjectOwnershipBucketOwnerEnforced {
		log.V(1).Info("Access control limits are disabled - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return Updated, nil
	}

	if bucket.Spec.ForProvider.ACL == nil &&
		bucket.Spec.ForProvider.AccessControlPolicy == nil &&
		bucket.Spec.ForProvider.GrantFullControl == nil &&
		bucket.Spec.ForProvider.GrantWrite == nil &&
		bucket.Spec.ForProvider.GrantWriteACP == nil &&
		bucket.Spec.ForProvider.GrantRead == nil &&
		bucket.Spec.ForProvider.GrantReadACP == nil {
		log.V(1).Info("No acl or access control policy or grants requested - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return Updated, nil
	}

	return NeedsUpdate, nil
}

// Implement Subresource interface

func (a *ACLClient) GetHandleErrorMsg() string {
	return errHandleAcl
}

func (a *ACLClient) GetSubresourceName() string {
	return "ACLClient"
}

func (a *ACLClient) HandleObservation(ctx context.Context, observation ResourceStatus, bucket *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	switch observation {
	case NoAction, Updated:
		return nil
	case NeedsUpdate, NeedsDeletion:
		return a.createOrUpdate(ctx, bucket, backendName)
	}
	return nil
}

func (a *ACLClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, a.log)

	log.Info("Updating acl", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := a.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutBucketAcl(ctx, s3Client, rgw.BucketToPutBucketACLInput(b))
	if err != nil {
		return err
	}

	return nil
}
