# OpenDataHub(ODH) Operator Release and Branching Process

This document provides an overview of the release process followed in the OpenDataHub (ODH) operator development lifecycle. It explains how upstream and downstream repositories are managed, including the branching strategy, code synchronization, and backporting workflows.

## Repositories Overview

The OpenDataHub operator development and release process spans two main repositories:

* **Upstream Repository:** [opendatahub-io/opendatahub-operator](https://github.com/opendatahub-io/opendatahub-operator)
* **Downstream Repository:** [red-hat-data-services/rhods-operator](https://github.com/red-hat-data-services/rhods-operator)

## Upstream Development and Release Workflow

### Main Branch

All new features and bug fixes are first merged into the `main` branch.

### RHOAI Branch (Upstream Tracking for Downstream)

* A dedicated rhoai branch exists to track downstream-related changes.
* Backporting from `main` to `rhoai` is typically automated using the `/cherry-pick` rhoai command on a pull request.
* If the automation fails, manual cherry-pick and a PR are required.

### Release Branches

* Release branches (`odh-*`, e.g., `odh-2.26.0`) are created from the `main` branch when a new upstream release is planned.

## Downstream Development and Release Workflow

### Main Branch

* Changes from the upstream `rhoai` branch are automatically synced into the downstream `main` branch.

### Release Branches

* From the downstream `main` branch, changes are backported to release-specific branches (`rhoai-*`,  e.g., `rhoai-2.21`).
* These branches represent versions that are under active development or maintenance.

### Code Freeze and Blocker-Only Phase

When a downstream release branch such as `rhoai-2.21` enters the code freeze phase:
* Automatic or the maual backports from `main` to `rhoai-2.21` stop.
* A new branch like `rhoai-2.22` is created for ongoing development and begins receiving backports from `main`.
* `rhoai-2.21` moves into the blocker-only phase, where only critical fixes are permitted.
* Any fix for `rhoai-2.21` must be approved as a blocker and manually backported from `main`.

### Z-Stream (Micro) Releases

* Older stable branches like `rhoai-2.20` receive critical fixes and updates via manual cherry-picking from the `main` branch.
* These are typically z-stream releases.

## Flow Diagram

```text 
main (upstream)
  |
  +--> odh-2.* (upstream release branches) 
  |
  v
rhoai (upstream -> to track downstream)
  |
  v
main (downstream)
  |
  +--> rhoai-2.22 (active development)
  |
  +--> rhoai-2.21 (code freeze -> blocker only)
  |
  +--> rhoai-2.20 (z-stream fixes)
  |
  +--> rhoai-2.* (downstream release branches)
```
**Note:** Branch versions shown in the diagram are examples.

## Reference

[Basic workflow for Operator](https://github.com/opendatahub-io/opendatahub-operator/blob/main/docs/sync_code.md)