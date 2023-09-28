Currently outgoing network and filesystem are disabled, because we compile static binary and mirrord is not able to catch file system requests.
Because it is't able to read resolver config properly, it is not able to resolve in-cluster host names.

For more info please follow: https://github.com/metalbear-co/mirrord/issues/1922
