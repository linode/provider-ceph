package s3clienthandler

import (
	"fmt"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/linode/provider-ceph/internal/utils/randomstring"
)

const (
	roleSessionNameSuffixLength         = 16
	roleSessionNameServiceNameMinLength = 1
	roleSessionNameServiceNameMaxLength = 29
	roleSessionNameTimestampFormat      = "20060102T150405Z"
)

var (
	errSuffixGenerationFailed = errors.New("failed generating random suffix")
	errServiceNameRequired    = errors.New("the service name is required")
	errServiceNameTooLong     = errors.New("the service name is too long")

	roleSessionNameSuffixCharset = randomstring.NewCharset("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
)

type roleSessionNameGenerator struct {
	now                   func() time.Time
	randomStringGenerator randomstring.Generator
}

func newRoleSessionNameGenerator() *roleSessionNameGenerator {
	return &roleSessionNameGenerator{
		now:                   time.Now,
		randomStringGenerator: &randomstring.StandardGenerator{},
	}
}

// Generate generates a unique and consistently structures role session name for
// use with STS AssumeRole requests. The service name must conform to the regex:
// [\w+=,.@-]{1,29}
//
// The format is:
//
//	<serviceName>-<timestamp>-<randomSuffix>
//	provider-ceph-202312122T124851Z-VdlyVlHrWkDG5pQj
func (r *roleSessionNameGenerator) generate(serviceName string) (string, error) {
	if len(serviceName) < roleSessionNameServiceNameMinLength {
		return "", errServiceNameRequired
	}
	if len(serviceName) > roleSessionNameServiceNameMaxLength {
		return "", errServiceNameTooLong
	}

	suffix, err := r.randomStringGenerator.Generate("", roleSessionNameSuffixLength, roleSessionNameSuffixCharset)
	if err != nil {
		return "", errors.Wrap(err, errSuffixGenerationFailed.Error())
	}

	return fmt.Sprintf("%s-%s-%s", serviceName, r.now().Format(roleSessionNameTimestampFormat), suffix), nil
}
