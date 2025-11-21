# E2E Test Suite Update Requirement Guidelines

When bringing new changes to the operator code, such changes are by default required to be accompanied by extending and/or updating the E2E test suite accordingly. A dedicated GitHub Action check is in place to enforce this requirement.

It is possible to opt-out of this check with proper justification. Please refer to the guidelines below to determine whether your PR justifies skipping this check, and how to do it (if applicable).

### Appropriate cases for opting-out of the E2E test suite update requirement:

- Documentation-only changes (README, comments, etc.)
- Unit test additions/modifications without functional changes
- Code style/formatting changes
- Dependency version updates without functional impact
- Build system changes that do not affect runtime behavior
- Non-functional refactoring with existing test coverage

### NOT Appropriate cases for opting-out of E2E test suite update requirement:

- New feature implementation
- Bug fixes affecting user-facing functionality
- API changes or modifications
- Configuration changes affecting deployment
- Changes to controllers, operators, or core logic
- Cross-component integration modifications
- Changes affecting user workflows or UI

### Opt-out guide
**Note:** This particular guide is also present in the default PR template description. In case you are looking to opt-out of the requirement, **it is highly recommended that your PR description adheres to the default PR template** - particularly, please keep the `### E2E test suite update requirement` section and set it up according to the steps below

1. Inspect the above-mentioned guidelines, to determine if the nature of the PR changes allows for skipping this requirement
2. Provide justification in the PR description under the `#### E2E update requirement opt-out justification` section
   - **Note:** In case your PR description does not adhere to our PR template and is missing the `### E2E test suite update requirement` section (or any of its subsections), you can find the original PR template in `.github/PULL_REQUEST_TEMPLATE.md`, and restore the missing section from there
3. Check the `Skip requirement to update E2E test suite for this PR` checkbox
4. Submit/save these changes to the PR description. This will automatically trigger the check.