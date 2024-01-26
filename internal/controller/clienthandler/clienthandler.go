package clienthandler

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	ceph "github.com/linode/provider-ceph/internal/ceph"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type S3ClientHandler struct {
	kubeClient    client.Client
	assumeRoleArn *string
	backendStore  *backendstore.BackendStore
	s3Timeout     time.Duration
	log           logging.Logger
}

func NewS3ClientHandler(options ...func(*S3ClientHandler)) *S3ClientHandler {
	c := &S3ClientHandler{}
	for _, o := range options {
		o(c)
	}

	return c
}

func WithKubeClient(k client.Client) func(*S3ClientHandler) {
	return func(c *S3ClientHandler) {
		c.kubeClient = k
	}
}

func WithAssumeRoleArn(a *string) func(*S3ClientHandler) {
	return func(c *S3ClientHandler) {
		c.assumeRoleArn = a
	}
}

func WithBackendStore(s *backendstore.BackendStore) func(*S3ClientHandler) {
	return func(c *S3ClientHandler) {
		c.backendStore = s
	}
}

func WithLog(l logging.Logger) func(*S3ClientHandler) {
	return func(c *S3ClientHandler) {
		c.log = l
	}
}

func WithS3Timeout(t time.Duration) func(*S3ClientHandler) {
	return func(r *S3ClientHandler) {
		r.s3Timeout = t
	}
}

func (c *S3ClientHandler) GetS3Client(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (backendstore.S3Client, error) {
	if c.assumeRoleArn != nil {
		return c.assumeRoleS3Client(ctx, bucket, backendName)
	}

	return c.backendStore.GetBackendS3Client(backendName), nil
}

func (c *S3ClientHandler) assumeRoleS3Client(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (backendstore.S3Client, error) {
	// TODO: implement proper role session name generation
	roleSessionName, err := generateRoleSessionName()
	if err != nil {
		return nil, err
	}

	input := &sts.AssumeRoleInput{
		RoleArn:         c.assumeRoleArn,
		RoleSessionName: &roleSessionName,
		Tags:            copySTSTags(bucket.Spec.ForProvider.AssumeRoleTags),
	}

	resp, err := ceph.AssumeRole(ctx, c.backendStore.GetBackendSTSClient(backendName), input)
	if err != nil {
		return nil, err
	}

	if resp.Credentials == nil || resp.Credentials.AccessKeyId == nil || resp.Credentials.SecretAccessKey == nil {
		return nil, errors.New("assume role response does not contain required credentials")
	}

	data := map[string][]byte{
		ceph.AccessKey: []byte(*resp.Credentials.AccessKeyId),
		ceph.SecretKey: []byte(*resp.Credentials.SecretAccessKey)}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: backendName}, pc); err != nil {
		return nil, err
	}

	return ceph.NewS3Client(ctx, data, &pc.Spec, c.s3Timeout)
}

// copySTSTags converts a list of local v1alpha1.Tags to STS Tags
func copySTSTags(tags []v1alpha1.Tag) []ststypes.Tag {
	out := make([]ststypes.Tag, 0)
	for _, tag := range tags {
		out = append(out, ststypes.Tag{Key: aws.String(tag.Key), Value: aws.String(tag.Value)})
	}

	return out
}

func generateRoleSessionName() (string, error) {
	const pattern = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_+=,.@-"
	var result string
	patternLength := big.NewInt(int64(len(pattern)))

	for i := 0; i < 63; i++ {
		randomIndex, err := rand.Int(rand.Reader, patternLength)
		if err != nil {
			return "", errors.Wrap(err, "failed to generate role session name")
		}
		result += string(pattern[randomIndex.Int64()])
	}

	return result, nil
}
