# Debug Logs

Provider Ceph supports `--zap-log-level` and `--zap-stacktrace-level` flags. The easiest way to set flags on a provider is to create a DeploymentRuntimeConfig and reference it from the Provider. See `./provider.yaml` and `./deployment-runtime-config.yaml` as an example.
