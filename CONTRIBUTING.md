# Contributing to the Opendatahub Operator

Thanks for your interest in the opendatahub-operator project! You can contribute to this project in various ways: filing bug reports, proposing features, submitting pull requests (PRs), and improving documentation.

Before you begin, please take a look at our contribution guidelines below to ensure that your contribuutions are aligned with the project's goals.

## Reporting Issues
Issues are tracked using [Jira](https://issues.redhat.com/secure/RapidBoard.jspa?rapidView=18680#). If you encounter a bug or have suggestions for enhancements, please follow the steps below:

1. **Check for Existing Issues:** Before creating a new issue, search the Jira project to see if a similar issue already exists.
2. **Create a Jira Ticket:** If the issue doesn’t exist, create a new ticket in Jira. 
   - **For Feature Requests:**  Set the issue type to be `Initative`
   - **For Bugs:** Set the issue type to `Bug`
   - **For all other code changes:** Use the issue type `Story`
   - Add "Platform" in "Components" field

## Pull Requests

### Workflow

1. **Fork the Repository:** Create your own fork of the repository to work on your changes.
2. **Create a Branch:** Create your own branch to include changes for the feature or a bug fix off of `main` branch.
3. **Work on Your Changes:** Commit often, and ensure your code adheres to these [Code Style Guidelines](#code-style-guidelines) and passes all the [quality gates](#quality-gates) for the operator.
4. **Testing:** Make sure your code passes all the tests, including any new tests you've added. And that your changes do not decrease the test coverage as shown on report. Every new feature should come with unit tests that cover that new part of the code.

### Open a Pull Request:

1. **Link to Jira Issue**: Include the Jira issue link in your PR description.
2. **Description**: Provide a detailed description of the changes and what they fix or implement.
3. **Add Testing Steps**: Provide information on how the PR has been tested, and list out testing steps if any for reviewers.
4. **Review Request**: Tag the relevant maintainers(@opendatahub-io/odh-operator-maintainers ) or team members(@opendatahub-io/odh-platform-members) for a review.
5. **Resolve Feedback**: Be open to feedback and iterate on your changes.

### Quality Gates

To ensure the contributed code adheres to the project goals, we have set up some automated quality gates:

1. [linters](https://github.com/opendatahub-io/opendatahub-operator/blob/main/.github/workflows/linter.yaml): Ensure the check for linters is successful. If it fails, run `make lint` to resolve errors
2. [api-docs](https://github.com/opendatahub-io/opendatahub-operator/blob/main/.github/workflows/check-file-updates.yaml): Ensure the api-docs are updated when making changes to operator apis. If it fails, run `make generate manifests api-docs` to resolve errors
3. [unit-tests](https://github.com/opendatahub-io/opendatahub-operator/blob/main/.github/workflows/unit-tests.yaml): Ensure unit tests pass. Run `make unit-tests`
4. [e2e-tests](https://prow.ci.openshift.org/job-history/gs/test-platform-results/pr-logs/directory/pull-ci-opendatahub-io-opendatahub-operator-main-opendatahub-operator-e2e): Ensure CI job for [e2e tests](https://github.com/opendatahub-io/opendatahub-operator/tree/main/tests/e2e) pass. Refer run e2e locally to debug. CI test logs can also be found under `Artifacts` directory under Job details.

### Code Style Guidelines

1. Follow the Go community’s best practices, which can be found in the official [Effective Go](https://go.dev/doc/effective_go) guide.
2. Follow the best practices defined by the [Operator SDK](https://sdk.operatorframework.io/docs/best-practices/).
3. Use `go fmt` to automatically format your code.
4. Ensure your code passes `make lint` (we have a .golangci.yml file configured in the repo).
5. Ensure you write clear and concise comments, especially for exported functions.
6. Always check and handle errors appropriately. Avoid ignoring errors by using _.
7. Make sure to run `go mod tidy` before submitting a PR to ensure the `go.mod` and `go.sum` files are up-to-date.

### Commit Messages

We follow the conventional commits format for writing commit messages. A good commit message should include:
1. **Type:** `fix`, `feat`, `docs`, `chore`, etc. **Note:** All commits except `chore` require an associated jira issue. Please add link to your jira issue.
2. **Scope:** A short description of the area affected.
3. **Summary:** A brief explanation of what the commit does.

### Testing PR Changes Locally

1. When a PR is opened, we have set up an OpenShift CI job that creates an operator image with the changes - `quay.io/opendatahub/opendatahub-operator:pr-<pr-number>`.
2. Set up your environment to override Makefile defaults as described [here](./docs/troubleshooting.md#using-a-localmk-file-to-override-makefile-variables-for-your-development-environment)
3. Use developer [guide](./README.md#developer-guide) to deploy operator [using OLM](./README.md#deployment) on a cluster.
4. Follow the steps given [here](./README.md#run-e2e-tests) to run e2e tests in your environment.

## Sync Changes in Downstream

After a PR is merged into the upstream `opendatahub-io/opendatahub-operator` repository, the changes need to be synced with the downstream repository:
detail see (./docs/sync_code.md#basic-workflow-for-operator)

## Communication

For general questions, feel free to open a discussion in our repository or communicate via:

- **Slack:** All issues related to ODH platform can be discussed in **#forum-openshift-ai-operator** channel.
- **Jira Comments**: Feel free to discuss issues directly on Jira tickets.