# Basic workflow for Operator

1. Create a branch:
Start by creating a branch from "main" branch.

2. Ensure local tests pass:
Run unit and e2e tests, and ensure they pass before submitting a pull request (PR) targeting "main" branch.
If the PR is associated with any Jira ticket, include the ticket link in the "Description" section of the PR.


3. Review and merge PR:
Once the PR has passed github checks and is reviewed and approved, the PR author should merge it into "main" branch.

4. Create a downstream sync PR:
The PR author should then create another PR targeting "rhoai" branch. Preferably, the PR title should include the prefix [sync], and the "rhoai" label should be added to help reviewers quickly acknowledge it.
If the PR is associated with any Jira ticket, include the ticket link in the "Description" section of the PR.

5. Merge sync PR:
After the sync PR has passed github checks and is reviewed and approved, the PR author should merge it into "rhoai" branch.

6. Automated sync:
Automation will sync changes to the RHDS rhods-operator on a daily basis.

# PR labels

These labels are optional but help streamline the review and release processes:

- odh-X.Y: Indicates that the change is planned for inclusion in the ODH release X.Y
- rhoai-X.Y: Specifies the target for a future downstream release. The PR may not need to merge immediately or might have lower review priority.
- rhoai: For [sync] PRs that bring changes from "main" branch to downstream.
- testing: Used for PRs focused solely on unit or e2e tests without any code changes (including code refactoring or improvements).
- documentation: changes on README.md, CONTRIBUTING.MD or files in `docs` folder 
- component/X: For integration work related to a specific component X. Component owners can be requested as reviewers if needed.
- multi-arch: Indicates changes specific to non-x64 architectures.