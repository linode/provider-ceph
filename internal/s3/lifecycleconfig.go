package s3

import (
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

// GenerateLifecycleConfiguration creates the PutBucketLifecycleConfigurationInput for the AWS SDK
func GenerateLifecycleConfigurationInput(name string, config *v1alpha1.BucketLifecycleConfiguration) *awss3.PutBucketLifecycleConfigurationInput {
	if config == nil {
		return nil
	}

	return &awss3.PutBucketLifecycleConfigurationInput{
		Bucket:                 aws.String(name),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{Rules: GenerateLifecycleRules(config.Rules)},
	}
}

// GenerateLifecycleRules creates the list of LifecycleRules for the AWS SDK
func GenerateLifecycleRules(in []v1alpha1.LifecycleRule) []types.LifecycleRule { //nolint:gocognit,gocyclo,cyclop // Function requires many checks.
	// NOTE(muvaf): prealloc is disabled due to AWS requiring nil instead
	// of 0-length for empty slices.
	var result []types.LifecycleRule //nolint:prealloc // NOTE(muvaf): prealloc is disabled due to AWS requiring nil instead of 0-length for empty slices.
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
				ExpiredObjectDeleteMarker: local.Expiration.ExpiredObjectDeleteMarker,
			}
			if local.Expiration.Days != nil {
				rule.Expiration.Days = *local.Expiration.Days
			}
			if local.Expiration.Date != nil {
				rule.Expiration.Date = &local.Expiration.Date.Time
			}
		}
		if local.NoncurrentVersionExpiration != nil {
			if local.NoncurrentVersionExpiration.NoncurrentDays != nil {
				rule.NoncurrentVersionExpiration = &types.NoncurrentVersionExpiration{NoncurrentDays: *local.NoncurrentVersionExpiration.NoncurrentDays}
			}
		}
		if local.NoncurrentVersionTransitions != nil {
			rule.NoncurrentVersionTransitions = make([]types.NoncurrentVersionTransition, 0)
			for _, transition := range local.NoncurrentVersionTransitions {
				nonCurrentVersionTransition := types.NoncurrentVersionTransition{}
				if transition.NoncurrentDays != nil {
					nonCurrentVersionTransition.NoncurrentDays = *transition.NoncurrentDays
				}
				if transition.NewerNoncurrentVersions != nil {
					nonCurrentVersionTransition.NewerNoncurrentVersions = *transition.NewerNoncurrentVersions
				}
				nonCurrentVersionTransition.StorageClass = types.TransitionStorageClass(transition.StorageClass)

				rule.NoncurrentVersionTransitions = append(rule.NoncurrentVersionTransitions, nonCurrentVersionTransition)
			}
		}
		if local.Transitions != nil {
			rule.Transitions = make([]types.Transition, 0)
			for _, localTransition := range local.Transitions {
				transition := types.Transition{}
				if localTransition.Days != nil {
					transition.Days = *localTransition.Days
				}
				if localTransition.Date != nil {
					transition.Date = &localTransition.Date.Time
				}

				transition.StorageClass = types.TransitionStorageClass(localTransition.StorageClass)
				rule.Transitions = append(rule.Transitions, transition)
			}
		}
		// This is done because S3 expects an empty filter, and never nil
		rule.Filter = &types.LifecycleRuleFilterMemberPrefix{}
		//nolint:nestif // Multiple checks required
		if local.Filter != nil {
			if local.Filter.Prefix != nil {
				rule.Filter = &types.LifecycleRuleFilterMemberPrefix{Value: *local.Filter.Prefix}
			}
			if local.Filter.Tag != nil {
				rule.Filter = &types.LifecycleRuleFilterMemberTag{Value: types.Tag{Key: aws.String(local.Filter.Tag.Key), Value: aws.String(local.Filter.Tag.Value)}}
			}
			if local.Filter.And != nil {
				andOperator := types.LifecycleRuleAndOperator{}
				if local.Filter.And.Prefix != nil {
					andOperator.Prefix = local.Filter.And.Prefix
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

// sortS3TagSet stable sorts an external s3 tag list by the key and value.
func sortS3TagSet(tags []types.Tag) []types.Tag {
	outTags := make([]types.Tag, len(tags))
	copy(outTags, tags)
	sort.SliceStable(outTags, func(i, j int) bool {
		return aws.ToString(outTags[i].Key) < aws.ToString(outTags[j].Key)
	})

	return outTags
}

func SortFilterTags(rules []types.LifecycleRule) {
	for i := range rules {
		andOperator, ok := rules[i].Filter.(*types.LifecycleRuleFilterMemberAnd)
		if ok {
			andOperator.Value.Tags = sortS3TagSet(andOperator.Value.Tags)
		}
	}
}
