# Testing

## Localstack
Due to its lightweight nature, [LocalStack](https://localstack.cloud/) is used as the s3 backend for testing.

A test setup with Localstack consists of the following:
- A single [Kind](https://kind.sigs.k8s.io/) cluster with [Crossplane](https://www.crossplane.io/) installed and `provider-ceph` deployed.
- Three [LocalStack](https://localstack.cloud/) instances deployed in the Kind cluster.

The tests are run using [Kuttl](https://kuttl.dev/) and s3 backend operations are verified using the [AWS CLI](https://aws.amazon.com/cli/).

This is the test setup used by Github Actions for this repo.
TODO: Update image as we no longer rely on Docker Compose. Localstack instances are now run within the Kind cluster.

![provider-ceph-testing drawio](https://user-images.githubusercontent.com/41484746/236199553-06990687-462a-4097-8d42-a7f7f055abbf.png)

## Run Kuttl Test Suite Against Localstack

```
make kuttl
```

## Ceph
A separate suite of tests can be run against a single Ceph cluster. These tests are not part of the Github Actions workflows for this repo. The Ceph cluster must be created separately and the keys & host base address are required as shown below.

## Run Kuttl Test Suite Against Ceph

```
AWS_ACCESS_KEY_ID=<your-access-key> AWS_SECRET_ACCESS_KEY=<your-secret-key> CEPH_ADDRESS=<your-ceph-cluster-address> make ceph-kuttl

```
