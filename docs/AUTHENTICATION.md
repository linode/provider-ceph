# Authentication Using AssumeRole

## Description

By default, Provider Ceph creates an S3 client for each Ceph Cluster using a single k8s `Secret` per Ceph Cluster. Each of these `Secrets` contain a Secret Key (SK) and Access Key (AK) pair and are referenced in the Ceph Cluster's corresponding `ProviderConfig`.


Provider Ceph can also be configured to use the Security Token Service (STS) AssumeRole action to retrieve a set of temporary credentials for S3 operations.
These temporary credentials are associated with the assumed role, and therefore inherit the permissions of the assumed role.

## How it Works
- Provider Ceph uses the existing `Secrets` to create an STS client per Ceph Cluster.
- When a Bucket is to be created or updated on a Ceph Cluster, the corresponding STS client sends an AssumeRole request to the Ceph Cluster.
- The AssumeRole response includes the temporary credentials.
- These temporary credentials are then used to create a new S3 client to the Ceph Cluster.
- The new "temporary" S3 client is used to perform the create or update operation.

## How to Enable AssumeRole Authentication
Set a RoleARN (Amazon Resource Name) value of your choice using the Provider Ceph flag:
```
--assume-role-arn="arn:aws:iam::<account_id>:role/<role_name>"
```

By setting this flag, Provider Ceph will use the STS AssumeRole API to assume the specified role for all bucket create and update operations. Delete operations are carried out by the default S3 client.

## BYO Authentication Service
Provider Ceph enables users to "bring-your-own" authentication service. STS AssumeRole requests can be diverted to an address other than that of the Ceph Cluster by specifying the address of your custom authentication service at `ProviderConfig.spec.stsAddress`. The authentication service implemented at this address by the user must be compliant with the [STS AssumeRole API](https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html).

Additionally, users can utilise the Bucket CR construct `Bucket.spec.assumeRoleTags` to attach Tags to the AssumeRole request. These tags can be used to hold metadata related to the bucket which can then be interpreted by the custom logic implemented in authentication service.
