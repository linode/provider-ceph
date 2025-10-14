#!/bin/bash

bucketname=$2
address=$3

if ! ping -c 1 ${address%:*} &>/dev/null; then
    # Resolve IP by node name.
    address="$(kubectl get no ${address%:*} -o jsonpath='{.status.addresses[0].address}'):${address#*:}"
fi

# Set AWS credentials for localstack
export AWS_ACCESS_KEY_ID=Dummy
export AWS_SECRET_ACCESS_KEY=Dummy
export AWS_DEFAULT_REGION=us-east-1

# Check whether the bucket already exists.
# We suppress all output - we're interested only in the return code.

bucket_exists() {
    aws --endpoint-url=http://$address s3api head-bucket \
        --bucket $bucketname \
            >/dev/null 2>&1

    if [[ ${?} -eq 0 ]]; then
        echo "pass: bucket $bucketname found"
        return 0
    else
        echo "error: bucket not found on $address"
        return 1
    fi
}

bucket_does_not_exist() {
    aws --endpoint-url=http://$address s3api head-bucket \
        --bucket $bucketname \
            >/dev/null 2>&1

    if [[ ${?} -eq 0 ]]; then
        echo "error: bucket found, should not exist"
        return 1
    else
        echo "pass: bucket does not exist"
        return 0
    fi
}

case "$1" in
  "") ;;
  bucket_exists) "$@"; exit;;
  bucket_does_not_exist) "$@"; exit;;

  *) echo "Unknown function: $1()"; exit 2;;
esac
