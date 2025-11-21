# How to run Integration Tests

The integration test Jenkins pipeline provides an
additional testing layer for pull requests that target the `main`
branch.

Unlike other automated tests (such as lint, unit, and e2e tests),
this integration test pipeline is **not automatically triggered** on a new pull request.
The main reason for this setup are resource usage and performance considerations.
For more details and usage guidelines, please refer to [the dedicated section](#resource-usage-and-performance-considerations).

## When Integration Tests are Required

Integration tests are **MANDATORY** for pull requests that modify:

- **Core Controller Logic** (`internal/controller/` changes)
- **API Definitions** (`api/` directory changes)
- **Webhook Implementations** (`internal/webhook/` changes)
- **Operator Configuration** (`config/` directory changes)
- **Feature Framework** (`pkg/feature/` changes)

Integration tests are **RECOMMENDED** for:
- Multi-component feature implementations
- Performance optimizations
- External dependency updates
- Documentation changes affecting workflows

## Resource usage and performance considerations

While using integration tests, please respect the following considerations and guidelines:

### Pipeline resource usage
- Integration tests pipeline consumes significant cluster resources
- Typical execution time: around 60 minutes
- Uses dedicated OpenShift clusters for testing
  - RHOAI Platform team owns two pre-configured clusters dedicated to integration testing
  - each test pipeline instance will be added to one of those clusters based on their respective availability
    - having those running clusters saves significant amount of time, as needing to allocate a new cluster for each pipeline instance would be additionally time-consuming (+60 minutes)

### Developer guidelines
- **Batch Related Changes**: Group related changes in a single PR to avoid multiple pipeline runs
- **Local Testing First**: Run unit tests locally before triggering integration tests
- **Component-Specific Impact**: Consider if your changes truly require full integration testing

### Rate limiting
- Only one integration test per PR at a time
- Multiple PRs may queue during high activity periods
- Plan accordingly for release deadlines

## Integration Test Architecture

The general outline of the integration test process is:

1. **Prow Command**: `/label run-integration-tests` applies the label
2. **GitHub Actions**: Builds operator, bundle, and catalog images with PR-specific tags
3. **Image Registry**: Pushes to quay.io (e.g., `quay.io/org/opendatahub-operator-catalog:pr-123`)
4. **Jenkins Trigger**: GitHub bot automatically comments `/test-integration`
5. **OpenShift Testing**: Jenkins deploys and tests on dedicated cluster
6. **Results**: Jenkins reports back with test results and artifacts

For precise step-by-step user guide, please refer to [the section below](#user-guide).

## User Guide
The user is in control of enabling and disabling the integration tests pipeline on their PR.
Once enabled, the test pipeline will trigger on any new commit push into the PR branch.
Step-by-step guide for enabling/disabling the test pipeline is provided below.

**To enable running the integration tests pipeline on a PR, please follow the steps below:**

1. **Label the PR as ready for integration tests**: comment `/label run-integration-tests` on the PR
2. **Wait for the bot**: `openshift-ci bot` will add the label to your PR
3. **Trigger the tests**: once PR is labeled, any new commit push into the PR branch will trigger the integration tests pipeline
4. **Monitor the image building process**: Keep an eye on the `Build Catalog FBC and run Integration tests` GitHub Action.
Once this action succeeds,
   `github-actions bot` will comment on the PR, which will automatically trigger the Jenkins pipeline
   - **Troubleshooting:** please refer to [the dedicated troubleshooting section](#image-buildpush-action-issues)
5. **Monitor the test process**: Once test pipeline starts in Jenkins, `rhods-ci-bot` will comment that the
tests have started, and provide a link to the Jenkins pipeline run details page
   - **Note:** Accessing the Jenkins pipeline run details requires active Red Hat VPN connection
   - **Troubleshooting:** please refer to [the dedicated troubleshooting sections](#jenkins-pipeline-not-triggering)
6. **Review the test pipeline results**: Once integration tests pipeline finishes in Jenkins, `rhods-ci-bot` will post the summary of test results as a PR comment. This comment includes:
   - links to the completed Jenkins jobs' details
     - **Note**: requires active Red Hat VPN connection to access
   - an overall test pipeline result and test pass rate
     - possible pipeline results:
       - `SUCCESS`: all executed tests passed
       - `UNSTABLE`: 80% <= test pass rate < 100%
       - `FAILED`: test pass rate < 80%
   - passed/failed/errors/skipped test counts
   - a link to the full test report
     - **Note**: requires active Red Hat VPN connection to access

**To disable running integration tests pipeline:**
1. **Remove the integration test PR label**: comment `/remove-label run-integration-tests` on the PR
2. **Wait for the bot**: `openshift-ci bot` will remove the label from your PR
3. Integration tests pipeline is now disabled and won't be triggered on future pushes (until re-enabled)

## Troubleshooting Common Issues

### Image build/push action issues
- Ensure the correct version tag was obtained
    - The version tag is obtained dynamically via a GitHub API call
        - This ensures that the correct latest tag will be used to construct image URLs
    - Expected format is`v<X>.<Y>.<Z>-pr-<pr_number>`
        - For example:`v2.32.0-pr-1`
- Ensure no conflicting image builds are happening at the same time for the same PR
- Verify `quay.io` registry permissions and quotas
- Check for base image availability and versions
- Ensure PR metadata was uploaded successfully as GitHub artifact

### Jenkins pipeline not triggering
- Verify the `/label run-integration-tests` command was successful
- Check that your changes affect monitored paths (`odh-bundle/`, `rhoai-bundle/`, `cmd/`, `config/`, `internal/`, `pkg/`)
- Ensure the GitHub Action `Build Catalog FBC and run Integration tests` completed successfully
- Look for the automated `/test-integration` comment from the `github-actions bot`

### Jenkins Pipeline failures
- Check Jenkins console logs for specific component failures
- Verify catalog image was built and pushed to `quay.io` successfully
- Review OpenShift cluster connectivity and permissions
