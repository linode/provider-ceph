#!/bin/bash

bucketname=$2
port=$3
# Check whether the bucket already exists. 
# We suppress all output - we're interested only in the return code.

bucket_exists() {
    aws --endpoint-url=http://localhost:$port s3api head-bucket \
        --bucket $bucketname \
            >/dev/null 2>&1

    if [[ ${?} -eq 0 ]]; then
        echo "pass: bucket $bucketname found"
        return 0
    else
        echo "error: bucket not found"
        return 1
    fi
}

bucket_does_not_exist() {
    aws --endpoint-url=http://localhost:$port s3api head-bucket \
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
