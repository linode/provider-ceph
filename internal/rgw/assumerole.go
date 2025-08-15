package rgw

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"go.opentelemetry.io/otel"
)

const (
	errAssumeRole = "failed to assume role"
)

func AssumeRole(ctx context.Context, stsClient backendstore.STSClient, input *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "AssumeRole")
	defer span.End()

	resp, err := stsClient.AssumeRole(ctx, input)
	if err != nil {
		err = errors.Wrap(err, errAssumeRole)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}
