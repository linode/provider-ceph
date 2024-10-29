package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"

	"go.opentelemetry.io/otel"
)

// ACLClient is the client for API methods and reconciling the ACL
type ACLClient struct {
	backendStore    *backendstore.BackendStore
	s3ClientHandler *s3clienthandler.Handler
	log             logging.Logger
}

// NewACLClient creates the client for ACL
func NewACLClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logging.Logger) *ACLClient {
	return &ACLClient{backendStore: b, s3ClientHandler: h, log: l}
}

func (l *ACLClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	_, span := otel.Tracer("").Start(ctx, "bucket.ACLClient.Observe")
	defer span.End()

	observationChan := make(chan ResourceStatus)

	for _, backendName := range backendNames {
		beName := backendName
		go func() {
			if l.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
				// If a backend is marked as unhealthy, we can ignore it for now by returning NoAction.
				// The backend may be down for some time and we do not want to block Create/Update/Delete
				// calls on other backends. By returning NeedsUpdate here, we would never pass the Observe
				// phase until the backend becomes Healthy or Disabled.
				observationChan <- NoAction

				return
			}
			observationChan <- l.observeBackend(bucket, beName)
		}()
	}

	for i := 0; i < len(backendNames); i++ {
		observation := <-observationChan
		if observation == NeedsUpdate || observation == NeedsDeletion {
			return observation, nil
		}
	}

	return Updated, nil
}

func (l *ACLClient) observeBackend(bucket *v1alpha1.Bucket, backendName string) ResourceStatus {
	l.log.Debug("Observing subresource acl on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	// If your bucket uses the bucket owner enforced setting for S3 Object
	// Ownership, ACLs are disabled and no longer affect permissions.
	if s3types.ObjectOwnership(aws.ToString(bucket.Spec.ForProvider.ObjectOwnership)) == s3types.ObjectOwnershipBucketOwnerEnforced {
		l.log.Debug("Access control limits are disabled - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return Updated
	}

	if bucket.Spec.ForProvider.ACL == nil &&
		bucket.Spec.ForProvider.AccessControlPolicy == nil &&
		bucket.Spec.ForProvider.GrantFullControl == nil &&
		bucket.Spec.ForProvider.GrantWrite == nil &&
		bucket.Spec.ForProvider.GrantWriteACP == nil &&
		bucket.Spec.ForProvider.GrantRead == nil &&
		bucket.Spec.ForProvider.GrantReadACP == nil {
		l.log.Debug("No acl or access control policy or grants requested - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return Updated
	}

	return NeedsUpdate
}

func (l *ACLClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.ACLClient.Handle")
	defer span.End()

	if l.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		traces.SetAndRecordError(span, errUnhealthyBackend)

		return errUnhealthyBackend
	}

	switch l.observeBackend(b, backendName) {
	case NoAction, Updated:
		return nil
	case NeedsUpdate, NeedsDeletion:
		if err := l.createOrUpdate(ctx, b, backendName); err != nil {
			err = errors.Wrap(err, errHandleAcl)
			traces.SetAndRecordError(span, err)

			return err
		}
	}

	return nil
}

func (l *ACLClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	l.log.Info("Updating acl", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutBucketAcl(ctx, s3Client, rgw.BucketToPutBucketACLInput(b))
	if err != nil {
		return err
	}

	return nil
}
