# Release Process

## Release Version
The release process for `provider-ceph` *mostly* follows the process described [here](https://github.com/crossplane/release#tldr-process-overview).

Here is a simplified set of steps to create a release for `provider-ceph`.

1. **feature freeze**: Merge all completed features into the main development branch to begin "feature freeze" period.
2. **pin dependencies**: Update the go module on the main development branch to depend on stable versions of dependencies if needed.
3. **tag release**: Run the **Tag** action on the main development branch with the desired version (e.g. `v0.0.2`).
    1. The action will create a release branch (e.g. `release-v0.0.2`), update the controller version and README, and create a tag with the release branch.
    2. The action also opens a PR against the main development branch. Please review/merge it to record the release.
4. **build/publish**: Run the **Publish** action on the release branch with the version that was just tagged. The released package will be published on the upbound marketplace [here](https://marketplace.upbound.io/account/linode/provider-ceph). 
5. **tag next pre-release**: Run the **Tag Release Candidate** action on the main development branch with `-rc.0` for the next release (e.g. `v0.0.3-rc.0`).

## Release Candidate
Every time there is a merge to `main`, the **Release Candidate Publish and PR** workflow will::
1. Create a release candidate package (eg `v0.0.3-rc.0.1.gcbf3f60is`) and publish it on the Upbound market place (Note: release candidates are not visible to the public on the Upbound marketplace).
2. Open a PR against the main development branch, updating the controller version and README to point at the latest release candidate.
