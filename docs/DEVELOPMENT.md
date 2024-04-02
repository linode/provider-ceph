# Development

Refer to Crossplane's [CONTRIBUTING.md] file for more information on how the
Crossplane community prefers to work. The [Provider Development][provider-dev]
guide may also be of use.

[CONTRIBUTING.md]: https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md

## Running Locally
Spin up the test environment, but with `provider-ceph` running locally in your terminal:

```
make dev
```

**or**


Spin up the test environment, but without Localstack and use your own external Ceph cluster instead. Also with `provider-ceph` running locally in your terminal:

```
AWS_ACCESS_KEY_ID=<your-access-key> AWS_SECRET_ACCESS_KEY=<yoursecret-key> CEPH_ADDRESS=<your-ceph-cluster-address> make dev-ceph
`

In either case, after you've made some changes, kill (Ctrl+C) the existing `provider-ceph` and re-run it:

```
make run
```

### Webhook Support
Running the validation webhook locally is a bit tricky, but it works out of the box.
Firt of all cluster provisioner script changes `ValidatingWebhookConfiguration`, to point to a
[localtunnel](https://github.com/localtunnel/localtunnel) instance (created by the script).
This endpoint has a valid TLS certification aprooved by Kubernetes, so validation requests should be served by the local process.

## Debugging Locally
Spin up the test environment, but with `provider-ceph` running locally in your terminal:

```
make mirrord.cluster mirrord.run
```

For debugging please install `mirrord` plugin in your IDE of choice.

### Webhook Support
Works out of the box. Validation requests goes to the original instance of the operator, but mirrord sends every network traffic to the local process instead.
