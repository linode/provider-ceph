package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"

	"github.com/crossplane/crossplane-runtime/pkg/errors"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"
	"go.opentelemetry.io/otel"
)

// PolicyClient is the client for API methods and reconciling a BucketPolicy
type PolicyClient struct {
	backendStore    *backendstore.BackendStore
	s3ClientHandler *s3clienthandler.Handler
	log             logr.Logger
}

func NewPolicyClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *PolicyClient {
	return &PolicyClient{backendStore: b, s3ClientHandler: h, log: l}
}

//nolint:dupl // LifecycleConfiguration and Policy are different feature.
func (p *PolicyClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.PolicyClient.Observe")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, p.log)

	observationChan := make(chan ResourceStatus)
	errChan := make(chan error)

	for _, backendName := range backendNames {
		beName := backendName
		go func() {
			if p.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
				// If a backend is marked as unhealthy, we can ignore it for now by returning NoAction.
				// The backend may be down for some time and we do not want to block Create/Update/Delete
				// calls on other backends. By returning NoAction here, we would never pass the Observe
				// phase until the backend becomes Healthy or Disabled.
				observationChan <- NoAction

				return
			}

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
			log.Info("Context timeout during bucket policy observation", consts.KeyBucketName, bucket.Name)
			err := errors.Wrap(ctx.Err(), errObservePolicy)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		case observation := <-observationChan:
			if observation == NeedsUpdate || observation == NeedsDeletion {
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

func (p *PolicyClient) observeBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	ctx, log := traces.InjectTraceAndLogger(ctx, p.log)

	log.V(1).Info("Observing subresource policy on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	s3Client, err := p.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
	if err != nil {
		return NeedsUpdate, err
	}

	// external keeps the bucket policy in backend.
	var external string

	response, err := rgw.GetBucketPolicy(ctx, s3Client, aws.String(bucket.Name))
	// If error is not NoSuchBucketPolicy error, return with the error.
	if err != nil && !isNoSuchBucketPolicy(err) {
		return NeedsUpdate, err
	}

	if response != nil && response.Policy != nil {
		external = *response.Policy
	}

	if bucket.Spec.ForProvider.Policy == "" {
		// No policy config is specified.
		// In that case, it should not exist on any backend.
		if external == "" {
			log.V(1).Info("No bucket policy found on backend - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return Updated, nil
		} else {
			log.V(1).Info("Bucket policy found on backend - requires deletion", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return NeedsDeletion, nil
		}
	}

	local := bucket.Spec.ForProvider.Policy
	if local != external {
		log.Info("Bucket policy requires update on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

func (p *PolicyClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.PolicyClient.Handle")
	defer span.End()

	if p.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		traces.SetAndRecordError(span, errUnhealthyBackend)

		return errUnhealthyBackend
	}

	observation, err := p.observeBackend(ctx, b, backendName)
	if err != nil {
		err = errors.Wrap(err, errHandlePolicy)
		traces.SetAndRecordError(span, err)

		return err
	}

	switch observation {
	case NoAction, Updated:
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

func (p *PolicyClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, p.log)

	log.Info("Updating bucket policy", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
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

func (p *PolicyClient) delete(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, p.log)

	log.Info("Deleting bucket policy", consts.KeyBucketName, b.Name, consts.KeyBackendName, backendName)
	s3Client, err := p.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	if err := rgw.DeleteBucketPolicy(ctx, s3Client, aws.String(b.Name)); err != nil {
		return err
	}

	return nil
}

func isNoSuchBucketPolicy(err error) bool {
	var ae smithy.APIError
	if !errors.As(err, &ae) {
		return false
	}

	return ae != nil && ae.ErrorCode() == "NoSuchBucketPolicy"
}
