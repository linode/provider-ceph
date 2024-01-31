package s3clienthandler

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/rgw"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	roleSessionNamePrefix = "provider-ceph"
)

var (
	errFailedToCreateAssumeRoleS3Client = errors.New("Failed to create s3 client via assume role")
	errNoSTSClient                      = errors.New("No STS client found for backend")
	errNoCreds                          = errors.New("AssumeRole response does not contain required credentials to create s3 client")
)

type Handler struct {
	kubeClient    client.Client
	assumeRoleArn *string
	backendStore  *backendstore.BackendStore
	s3Timeout     time.Duration
	log           logging.Logger
}

func NewHandler(options ...func(*Handler)) *Handler {
	h := &Handler{}
	for _, o := range options {
		o(h)
	}

	return h
}

func WithKubeClient(k client.Client) func(*Handler) {
	return func(h *Handler) {
		h.kubeClient = k
	}
}

func WithAssumeRoleArn(a *string) func(*Handler) {
	return func(h *Handler) {
		h.assumeRoleArn = a
	}
}

func WithBackendStore(s *backendstore.BackendStore) func(*Handler) {
	return func(h *Handler) {
		h.backendStore = s
	}
}

func WithS3Timeout(t time.Duration) func(*Handler) {
	return func(h *Handler) {
		h.s3Timeout = t
	}
}

func WithLog(l logging.Logger) func(*Handler) {
	return func(h *Handler) {
		h.log = l
	}
}

func (h *Handler) GetS3Client(ctx context.Context, b *v1alpha1.Bucket, backendName string) (backendstore.S3Client, error) {
	if h.assumeRoleArn != nil && *h.assumeRoleArn != "" {
		return h.createAssumeRoleS3Client(ctx, b, backendName)
	}

	return h.backendStore.GetBackendS3Client(backendName), nil
}

func (h *Handler) createAssumeRoleS3Client(ctx context.Context, b *v1alpha1.Bucket, backendName string) (backendstore.S3Client, error) {
	roleSessionName, err := newRoleSessionNameGenerator().generate(roleSessionNamePrefix)
	if err != nil {
		return nil, errors.Wrap(err, errFailedToCreateAssumeRoleS3Client.Error())
	}

	copiedTags := make([]ststypes.Tag, 0)
	if b.Spec.ForProvider.AssumeRoleTags != nil {
		copiedTags = copySTSTags(b.Spec.ForProvider.AssumeRoleTags)
	}

	input := &sts.AssumeRoleInput{
		RoleArn:         h.assumeRoleArn,
		RoleSessionName: &roleSessionName,
		Tags:            copiedTags,
	}

	stsClient := h.backendStore.GetBackendSTSClient(backendName)
	if stsClient == nil {
		return nil, errors.Wrap(errNoSTSClient, errFailedToCreateAssumeRoleS3Client.Error())
	}

	resp, err := rgw.AssumeRole(ctx, stsClient, input)
	if err != nil {
		return nil, errors.Wrap(err, errFailedToCreateAssumeRoleS3Client.Error())
	}

	if resp.Credentials == nil || resp.Credentials.AccessKeyId == nil || resp.Credentials.SecretAccessKey == nil {
		return nil, errors.Wrap(errNoCreds, errFailedToCreateAssumeRoleS3Client.Error())
	}

	data := map[string][]byte{
		consts.KeyAccessKey: []byte(*resp.Credentials.AccessKeyId),
		consts.KeySecretKey: []byte(*resp.Credentials.SecretAccessKey)}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := h.kubeClient.Get(ctx, types.NamespacedName{Name: backendName}, pc); err != nil {
		return nil, errors.Wrap(err, errFailedToCreateAssumeRoleS3Client.Error())
	}

	s3Client, err := rgw.NewS3Client(ctx, data, &pc.Spec, h.s3Timeout)
	if err != nil {
		return nil, errors.Wrap(err, errFailedToCreateAssumeRoleS3Client.Error())
	}

	return s3Client, nil
}

// copySTSTags converts a list of local v1alpha1.Tags to STS Tags
func copySTSTags(tags []v1alpha1.Tag) []ststypes.Tag {
	out := make([]ststypes.Tag, 0)
	for _, tag := range tags {
		out = append(out, ststypes.Tag{Key: aws.String(tag.Key), Value: aws.String(tag.Value)})
	}

	return out
}
