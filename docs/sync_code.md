# Basic workflow for Operator

There are two long-lived branches:

- `main` the primary development branch where all changes land first
- `rhoai` the branch tracking downstream (productization) changes

Changes to the operator should land in both `main` and the downstream `rhoai` branch.

```mermaid
gitGraph
    branch rhoai order: 4
    checkout main
    commit
    checkout rhoai
    commit
    checkout main
    branch feature
    commit id: "2-0a10a11"
    checkout main
    merge feature
    checkout rhoai
    branch cherry-pick-feature
    cherry-pick id:"2-0a10a11"
    checkout rhoai
    merge cherry-pick-feature
```

1. **Merge PR to `main`**. Follow the process in [CONTRIBUTING.md](../CONTRIBUTING.md).

2. **Create a downstream sync PR:**
The PR author should then create another PR targeting the `rhoai` branch. CI automation can usually create the
cherry-pick PR. Add a `/cherry-pick rhoai` comment to the original PR. CI will comment success or failure. If it fails,
you must manually cherry-pick commits to a new cherry-pick branch and manually open a new PR.

3. **Edit the cherry-pick PR:** Edit the title to include the prefix `[sync]`. If the PR is associated with any Jira
ticket, edit the description to include the ticket link.

4. **Merge sync PR:**
After the sync PR has passed github checks and is reviewed and approved, CI will merge it into `rhoai` branch.
