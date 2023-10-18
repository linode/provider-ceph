Testing on Localstack

Dependencies:

 * Kubectl

Start Localstack:

```bash
kubectl apply -f localstack-deployment.yaml
```

Apply provider config:

```bash
kubectl apply -f localstack-provider-cfg.yaml
```
