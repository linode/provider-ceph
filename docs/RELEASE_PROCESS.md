# Release Process

The release process for `provider-ceph` *mostly* follows the process described [here](https://github.com/crossplane/crossplane/blob/master/contributing/release-process.md).

Here is a simplified set of steps to create a release for `provider-ceph`.

1. **feature freeze**: Merge all completed features into main development branch of all repos to begin "feature freeze" period.
2. **pin dependencies**: Update the go module on main development branch to depend on stable versions of dependencies if needed.
3. **branch repo**: Create a new release branch using the GitHub UI for the repo.
4. **release branch prep**: Make any release-specific updates on the release branch.
   
   **Important**: Set the controller image version to the release version (e.g. v0.0.2) in `package/crossplane.yaml` https://github.com/linode/provider-ceph/blob/d851260fc3480e9b7bb36064516e289eb734a036/package/crossplane.yaml#L15
   
   This is a required step due to an issue described here which is due to be fixed.
   
6. **tag release**: Run the Tag action on the release branch with the desired version (e.g. v0.0.2).
7. **build/publish**: Run the CI and Configurations action on the release branch with the version that was just tagged. The released package will be published on the upbound marketplace [here](https://marketplace.upbound.io/account/linode/provider-ceph). 
