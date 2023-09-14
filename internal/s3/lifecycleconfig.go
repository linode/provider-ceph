package s3

import (
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

// GenerateLifecycleConfiguration creates the PutBucketLifecycleConfigurationInput for the AWS SDK
func GenerateLifecycleConfiguration(name string, config *v1alpha1.BucketLifecycleConfiguration) *awss3.PutBucketLifecycleConfigurationInput {
	if config == nil {
		return nil
	}
	return &awss3.PutBucketLifecycleConfigurationInput{
		Bucket:                 aws.String(name),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{Rules: GenerateLifecycleRules(config.Rules)},
	}
}

// GenerateLifecycleRules creates the list of LifecycleRules for the AWS SDK
func GenerateLifecycleRules(in []v1alpha1.LifecycleRule) []types.LifecycleRule { // nolint:gocyclo
	// NOTE(muvaf): prealloc is disabled due to AWS requiring nil instead
	// of 0-length for empty slices.
	var result []types.LifecycleRule // nolint:prealloc
	for _, local := range in {
		rule := types.LifecycleRule{
			ID:     local.ID,
			Status: types.ExpirationStatus(local.Status),
		}
		if local.AbortIncompleteMultipartUpload != nil {
			rule.AbortIncompleteMultipartUpload = &types.AbortIncompleteMultipartUpload{
				DaysAfterInitiation: local.AbortIncompleteMultipartUpload.DaysAfterInitiation,
			}
		}
		if local.Expiration != nil {
			rule.Expiration = &types.LifecycleExpiration{
				Days:                      local.Expiration.Days,
				ExpiredObjectDeleteMarker: local.Expiration.ExpiredObjectDeleteMarker,
			}
			if local.Expiration.Date != nil {
				rule.Expiration.Date = &local.Expiration.Date.Time
			}
		}
		if local.NoncurrentVersionExpiration != nil {
			rule.NoncurrentVersionExpiration = &types.NoncurrentVersionExpiration{NoncurrentDays: local.NoncurrentVersionExpiration.NoncurrentDays}
		}
		if local.NoncurrentVersionTransitions != nil {
			rule.NoncurrentVersionTransitions = make([]types.NoncurrentVersionTransition, len(local.NoncurrentVersionTransitions))
			for tIndex, transition := range local.NoncurrentVersionTransitions {
				rule.NoncurrentVersionTransitions[tIndex] = types.NoncurrentVersionTransition{
					NoncurrentDays: transition.NoncurrentDays,
					StorageClass:   types.TransitionStorageClass(transition.StorageClass),
				}
			}
		}
		if local.Transitions != nil {
			rule.Transitions = make([]types.Transition, len(local.Transitions))
			for tIndex, transition := range local.Transitions {
				rule.Transitions[tIndex] = types.Transition{
					Days:         transition.Days,
					StorageClass: types.TransitionStorageClass(transition.StorageClass),
				}
				if transition.Date != nil {
					rule.Transitions[tIndex].Date = &transition.Date.Time
				}
			}
		}
		// This is done because S3 expects an empty filter, and never nil
		rule.Filter = &types.LifecycleRuleFilterMemberPrefix{}
		if local.Filter != nil {
			if local.Filter.Prefix != nil {
				rule.Filter = &types.LifecycleRuleFilterMemberPrefix{Value: *local.Filter.Prefix}
			}
			if local.Filter.Tag != nil {
				rule.Filter = &types.LifecycleRuleFilterMemberTag{Value: types.Tag{Key: aws.String(local.Filter.Tag.Key), Value: aws.String(local.Filter.Tag.Value)}}
			}
			if local.Filter.And != nil {
				andOperator := types.LifecycleRuleAndOperator{
					Prefix: local.Filter.And.Prefix,
				}
				if local.Filter.And.Tags != nil {
					andOperator.Tags = sortS3TagSet(copyTags(local.Filter.And.Tags))
				}
				rule.Filter = &types.LifecycleRuleFilterMemberAnd{Value: andOperator}
			}
		}
		result = append(result, rule)
	}
	return result
}

// copyTags converts a list of local v1beta.Tags to S3 Tags
func copyTags(tags []v1alpha1.Tag) []types.Tag {
	out := make([]types.Tag, 0)
	for _, one := range tags {
		out = append(out, types.Tag{Key: aws.String(one.Key), Value: aws.String(one.Value)})
	}
	return out
}

// copyAWSTags converts a list of external s3.Tags to local Tags
func copyAWSTags(tags []types.Tag) []v1alpha1.Tag {
	out := make([]v1alpha1.Tag, len(tags))
	for i, one := range tags {
		out[i] = v1alpha1.Tag{Key: aws.ToString(one.Key), Value: aws.ToString(one.Value)}
	}
	return out
}

// sortS3TagSet stable sorts an external s3 tag list by the key and value.
func sortS3TagSet(tags []types.Tag) []types.Tag {
	outTags := make([]types.Tag, len(tags))
	copy(outTags, tags)
	sort.SliceStable(outTags, func(i, j int) bool {
		return aws.ToString(outTags[i].Key) < aws.ToString(outTags[j].Key)
	})
	return outTags
}

func sortFilterTags(rules []types.LifecycleRule) {
	for i := range rules {
		andOperator, ok := rules[i].Filter.(*types.LifecycleRuleFilterMemberAnd)
		if ok {
			andOperator.Value.Tags = sortS3TagSet(andOperator.Value.Tags)
		}
	}
}
