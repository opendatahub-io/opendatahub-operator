<!--- 
Many thanks for submitting your Pull Request ❤️!

Please complete the following sections for a smooth review.
-->

## Description
<!--- Describe your changes in detail -->

<!--- Link your JIRA and related links here for reference. -->

## How Has This Been Tested?
<!--- Please describe in detail how you tested your changes. -->
<!--- Include details of your testing environment and the tests you ran to -->
<!--- see how your change affects other areas of the code, etc. -->

## Screenshot or short clip
<!--- If applicable, attach a screenshot or a short clip demonstrating the feature. -->

## Merge criteria
<!--- This PR will be merged by any repository approver when it meets all the points in the checklist -->
<!--- Go over all the following points, and put an `x` in all the boxes that apply. -->

- [ ] You have read the [contributors guide](https://github.com/opendatahub-io/opendatahub-operator/blob/incubation/CONTRIBUTING.md).
- [ ] Commit messages are meaningful - have a clear and concise summary and detailed explanation of what was changed and why.
- [ ] Pull Request contains a description of the solution, a link to the JIRA issue, and to any dependent or related Pull Request.
- [ ] Testing instructions have been added in the PR body (for PRs involving changes that are not immediately obvious).
- [ ] The developer has manually tested the changes and verified that the changes work
- [ ] The developer has run the integration test pipeline and verified that it passed successfully

### E2E test suite update requirement

When bringing new changes to the operator code, such changes are by default required to be accompanied by extending and/or updating the E2E test suite accordingly.

To opt-out of this requirement:
1. **Please inspect the [opt-out guidelines](https://github.com/opendatahub-io/opendatahub-operator/blob/main/docs/e2e-update-requirement-guidelines.md)**, to determine if the nature of the PR changes allows for skipping this requirement
2. If opt-out is applicable, create opt-out justification PR comment
   - start the comment with `## E2E update requirement opt-out justification:` title, and provide a short summary of reasons for opting-out of this requirement
3. Edit this PR description by checking the checkbox below:
- [ ] Skip requirement to update E2E test suite for this PR
