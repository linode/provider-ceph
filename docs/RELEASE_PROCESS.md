# Release Process

The release process for `provider-ceph` *mostly* follows the process described [here](https://github.com/crossplane/release#tldr-process-overview).

Here is a simplified set of steps to create a release for `provider-ceph`.

1. **feature freeze**: Merge all completed features into main development branch of all repos to begin "feature freeze" period.
2. **pin dependencies**: Update the go module on main development branch to depend on stable versions of dependencies if needed.
3. **tag release**: Run the Tag action on the main development branch with the desired version (e.g., v0.0.2).
    1. The action will create a release branch (e.g., release-v0.0.2), update the controller version and README, and make a tag with the release branch.
    2. The action also make a PR to the main development branch. Please review/merge it to record the release.
4. **build/publish**: Run the CI and Configurations action on the release branch with the version that was just tagged. The released package will be published on the upbound marketplace [here](https://marketplace.upbound.io/account/linode/provider-ceph). 
