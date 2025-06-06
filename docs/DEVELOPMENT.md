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
```

In either case, after you've made some changes, kill (Ctrl+C) the existing `provider-ceph` and re-run it:

```
make run
```

### Webhook Support for Local Development
Running the validation webhook locally is a bit tricky, but it works out of the box.
Under the hood, a [localtunnel](https://github.com/localtunnel/localtunnel) instance is created and the `ValidatingWebhookConfiguration` is updated to point to the localtunnel. This endpoint has a valid TLS certification approved by Kubernetes, so validation requests are served by the local process.
