package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"

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

// PolicyClient is the client for API methods and reconciling the BucketPolicy
type BucketPolicyClient struct {
	backendStore    *backendstore.BackendStore
	s3ClientHandler *s3clienthandler.Handler
	log             logging.Logger
}

// NewBucketPolicyClient creates the client for Accelerate Configuration
func NewBucketPolicyClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logging.Logger) *BucketPolicyClient {
	return &BucketPolicyClient{backendStore: b, s3ClientHandler: h, log: l}
}

func (p *BucketPolicyClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.BucketPolicyClient.Observe")
	defer span.End()

	observationChan := make(chan ResourceStatus)
	errChan := make(chan error)

	for _, backendName := range backendNames {
		beName := backendName
		go func() {
			observation, err := p.observeBackend(ctx, bucket, beName)
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
			p.log.Info("Context timeout during bucket policy observation", consts.KeyBucketName, bucket.Name)
			err := errors.Wrap(ctx.Err(), errObservePolicy)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		case observation := <-observationChan:
			if observation != Updated {
				return observation, nil
			}
		case err := <-errChan:
			err = errors.Wrap(err, errObservePolicy)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		}
	}

	return Updated, nil
}

func (p *BucketPolicyClient) observeBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	p.log.Info("Observing subresource policy on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	if p.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		// If a backend is marked as unhealthy, we can ignore it for now by returning Updated.
		// The backend may be down for some time and we do not want to block Create/Update/Delete
		// calls on other backends. By returning NeedsUpdate here, we would never pass the Observe
		// phase until the backend becomes Healthy or Disabled.
		return Updated, nil
	}

	s3Client, err := p.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
	if err != nil {
		return NeedsUpdate, err
	}

	response, err := rgw.GetBucketPolicy(ctx, s3Client, aws.String(bucket.Name))
	if err != nil {
		return NeedsUpdate, err
	}

	if bucket.Spec.ForProvider.BucketPolicy == "" {
		// No policy config is specified.
		// In that case, it should not exist on any backend.
		if *response.Policy == "" {
			p.log.Info("No bucket policy found on backend - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return Updated, nil
		} else {
			p.log.Info("Bucket policy found on backend - requires deletion", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return NeedsDeletion, nil
		}
	}

	// TODO: Ensure how to compare
	local := bucket.Spec.ForProvider.BucketPolicy

	external := *response.Policy

	if external != "" && local == "" {
		return NeedsUpdate, nil
	}

	if local != external {
		p.log.Info("Bucket policy requires update on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

func (p *BucketPolicyClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.BucketPolicyClient.Handle")
	defer span.End()

	observation, err := p.observeBackend(ctx, b, backendName)
	if err != nil {
		err = errors.Wrap(err, errHandlePolicy)
		traces.SetAndRecordError(span, err)

		return err
	}

	switch observation {
	case Updated:
		return nil
	case NeedsDeletion:
		if err := p.delete(ctx, b, backendName); err != nil {
			err = errors.Wrap(err, errHandlePolicy)

			traces.SetAndRecordError(span, err)

			return err
		}
	case NeedsUpdate:
		if err := p.createOrUpdate(ctx, b, backendName); err != nil {
			err = errors.Wrap(err, errHandlePolicy)

			traces.SetAndRecordError(span, err)

			return err
		}
	}

	return nil
}

func (p *BucketPolicyClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	p.log.Info("Updating bucket policy", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := p.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutBucketPolicy(ctx, s3Client, b)
	if err != nil {
		return err
	}

	return nil
}

func (p *BucketPolicyClient) delete(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	p.log.Info("Deleting bucket policy", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := p.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	if err := rgw.DeleteBucketPolicy(ctx, s3Client, aws.String(b.Name)); err != nil {
		return err
	}

	return nil
}
