Testing on Localstack

Dependencies:

 * AWS CLI
 * Docker-compose (`compose` subcommand of `docker` doesn't work)
 * S3cmd (optional)
 * Kubectl

Create an AWS account, it should be real or fake.

```bash
aws configure
```

Suggested config:

```
AWS Access Key ID: Dummy
AWS Secret Access Key: Dummy
Default region name: us-east-1
```

Edit your `bash` profile (optional):

```bash
export AWS_ACCESS_KEY_ID=Dummy
export AWS_SECRET_ACCESS_KEY=Dummy
export AWS_DEFAULT_REGION=us-east-1
```

Generate client config for `s3cmd` (optional):

```bash
s3cmd --configure
```

Start Localstack:

```bash
docker-compose -f docker-compose.yml up
```

Apply provider config:

```bash
kubectl apply -f config.yaml
```
