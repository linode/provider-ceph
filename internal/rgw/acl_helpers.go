package rgw

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

func BucketToPutBucketACLInput(bucket *v1alpha1.Bucket) *s3.PutBucketAclInput {
	putBucketAclInput := &s3.PutBucketAclInput{}
	putBucketAclInput.ACL = s3types.BucketCannedACL(aws.ToString(bucket.Spec.ForProvider.ACL))
	putBucketAclInput.Bucket = aws.String(bucket.Name)

	if bucket.Spec.ForProvider.AccessControlPolicy != nil {
		aclPolicy := &types.AccessControlPolicy{}
		if bucket.Spec.ForProvider.AccessControlPolicy.Grants != nil {
			aclPolicy.Grants = GenerateGrants(bucket.Spec.ForProvider.AccessControlPolicy.Grants)
		}
		if bucket.Spec.ForProvider.AccessControlPolicy.Owner != nil {
			aclPolicy.Owner = GenerateOwner(bucket.Spec.ForProvider.AccessControlPolicy.Owner)
		}

		putBucketAclInput.AccessControlPolicy = aclPolicy
	}

	return putBucketAclInput
}

func GenerateAccessControlPolicy(policyIn *v1alpha1.AccessControlPolicy) *types.AccessControlPolicy {
	return &types.AccessControlPolicy{
		Grants: GenerateGrants(policyIn.Grants),
		Owner:  GenerateOwner(policyIn.Owner),
	}
}

func GenerateGrants(grantsIn []v1alpha1.Grant) []types.Grant {
	grantsOut := make([]types.Grant, 0)

	for _, grantIn := range grantsIn {
		localGrant := types.Grant{}
		if grantIn.Grantee != nil {
			localGrant.Grantee = &types.Grantee{}
			if grantIn.Grantee.DisplayName != nil {
				localGrant.Grantee.DisplayName = grantIn.Grantee.DisplayName
			}
			if grantIn.Grantee.EmailAddress != nil {
				localGrant.Grantee.EmailAddress = grantIn.Grantee.EmailAddress
			}
			if grantIn.Grantee.ID != nil {
				localGrant.Grantee.ID = grantIn.Grantee.ID
			}
			if grantIn.Grantee.URI != nil {
				localGrant.Grantee.URI = grantIn.Grantee.URI
			}
			localGrant.Permission = types.Permission(grantIn.Permission)
			// Type is required.
			localGrant.Grantee.Type = types.Type(grantIn.Grantee.Type)
		}
		grantsOut = append(grantsOut, localGrant)
	}

	return grantsOut
}

func GenerateOwner(ownerIn *v1alpha1.Owner) *types.Owner {
	ownerOut := &types.Owner{}
	if ownerIn.DisplayName != nil {
		ownerOut.DisplayName = ownerIn.DisplayName
	}
	if ownerIn.ID != nil {
		ownerOut.ID = ownerIn.ID
	}

	return ownerOut
}

func BucketToGetBucketACLInput(bucket *v1alpha1.Bucket) *s3.GetBucketAclInput {
	return &s3.GetBucketAclInput{
		Bucket: aws.String(bucket.Name),
	}
}
