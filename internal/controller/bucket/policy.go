package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"
)

// PolicyClient is the client for API methods and reconciling a BucketPolicy
type PolicyClient struct {
	BaseSubresourceClient
}

func NewPolicyClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *PolicyClient {
	return &PolicyClient{BaseSubresourceClient: NewBaseSubresourceClient(b, h, l)}
}

//nolint:dupl // Policy is a different feature.
func (p *PolicyClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	return p.BaseSubresourceClient.Observe(ctx, bucket, backendNames, p)
}

func (p *PolicyClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	return p.BaseSubresourceClient.Handle(ctx, b, backendName, bb, p)
}

// Implement Subresource interface

func (p *PolicyClient) GetLogger() logr.Logger {
	return p.log
}

func (p *PolicyClient) GetBackendStore() *backendstore.BackendStore {
	return p.backendStore
}

func (p *PolicyClient) GetS3ClientHandler() *s3clienthandler.Handler {
	return p.s3ClientHandler
}

func (p *PolicyClient) GetObserveErrorMsg() string {
	return errObservePolicy
}

func (p *PolicyClient) ObserveBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
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

// Implement Subresource interface

func (p *PolicyClient) GetHandleErrorMsg() string {
	return errHandlePolicy
}

func (p *PolicyClient) GetSubresourceName() string {
	return "PolicyClient"
}

func (p *PolicyClient) HandleObservation(ctx context.Context, observation ResourceStatus, bucket *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	switch observation {
	case NoAction, Updated:
		return nil
	case NeedsDeletion:
		return p.delete(ctx, bucket, backendName)
	case NeedsUpdate:
		return p.createOrUpdate(ctx, bucket, backendName)
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
