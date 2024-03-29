<!--
Thank you for helping to improve Provider Ceph!

Please read through https://git.io/fj2m9 if this is your first time opening a
Crossplane pull request. Find us in https://slack.crossplane.io/messages/dev if
you need any help contributing.
-->

### Description of your changes

<!--
Briefly describe what this pull request does. Be sure to direct your reviewers'
attention to anything that needs special consideration.
-->

I have:

- [ ] Run `make reviewable` to ensure this PR is ready for review.
- [ ] Run `make ceph-kuttl` to validate these changes against Ceph. This step is not always necessary. However, for changes related to S3 calls it is sensible to validate against an actual Ceph cluster. Localstack is used in our CI Kuttl suite for convenience and there can be disparity in S3 behaviours betwee it and Ceph. See `docs/TESTING.md` for information on how to run tests against a Ceph cluster.
- [ ] Added `backport release-x.y` labels to auto-backport this PR if necessary.

### How has this code been tested

<!--
Before reviewers can be confident in the correctness of this pull request, it
needs to tested and shown to be correct. Briefly describe the testing that has
already been done or which is planned for this change.
-->

[contribution process]: https://git.io/fj2m9
