# Open Data Hub Operator

## Syncing With Upstream

The goal is to sync this repository with upstream ([kfctl](https://github.com/kubeflow/kfctl)) weekly. At the moment this process it manual, but we will investigate how to automate it.

You can find a helper script automating part of the process to sync with the upstream [here](scripts/rebase.sh).

The script requires `GITHUB_TOKEN` environment variable to be set (see [Creating a personal access token](https://docs.github.com/en/github/authenticating-to-github/creating-a-personal-access-token)).


### Testing the Rebase on Personal Fork

You can try the rebase against your own for of kfctl repository fork by running the following commands:

```
GITHUB_TOKEN=<your_github_token> TARGET_REPO=<url_of_your_kfctl_fork> scripts/rebase.sh
```

This will rebase `master` branch of the `TARGET_REPO` repository on top of upstream repository, build the operator image, push it to `quay.io/${USER}/opendatahub-operator` registry repository and generate a PR against `master` branch of the `TARGET_REPO` repository. The link to the new PR should be printed out at the end of the script run:

```
Rebase PR created at https://github.com/<user>/kfctl/pull/13
```

### Creating a Rebase PR

To create a sync/rebase PR against this repository, run the rebase.sh script with only specifying the `GITHUB_TOKEN`:

```
GITHUB_TOKEN=<your_github_token> scripts/rebase.sh
```

### Additional Options

You can override a few options in the script:

* `UPSTREAM` - the URL of upstream repository
* `TARGET_REPO` - the URL of PR target repository
* `REBASE_TO` - a specific commit to use for the rebase
* `OPERATOR_IMG` - URL of the operator image which should be pushed after the build
* `PUSH_OPERATOR` - will skip operator image build if set to `false`
