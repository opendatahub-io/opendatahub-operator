# Automated Manifest Updates

This document describes the automated systems for updating OpenDataHub component manifest references.

## Overview

The `manifests-config.yaml` file contains references to various OpenDataHub component repositories.
Since our e2e tests rely on these references, they need to be kept up to date but pointing to a branch instead of a specific commit SHA could lead to breakages in e2e tests, and block PRs to being merged.

These references can become outdated as components receive updates, so we provide an automated solution to keep these references current.

## Reference Updates Workflow

It will be executed by a Github Action workflow.

### Location

`.github/workflows/update-manifest-shas.yml`

### Features

- ✅ Runs daily at 6:00 AM UTC
- ✅ Can be triggered manually via workflow_dispatch
- ✅ Only updates components using branch@sha tracking format
- ✅ Fetches latest commit SHAs for tracked branches
- ✅ Batch updates (all changes in single PR)
- ✅ Automatic branch cleanup after PR merge
- ✅ Works directly with manifests-config.yaml format

## Configuration Details

### Components Tracked

The automation only processes components that use the branch@sha tracking format:

- **Components with branch@sha format** (e.g., `main@a1b2c3d4`) - Automatically updated to latest commit SHA
- **Components without @ format** (plain branches, tags) - Skipped

Examples from `manifests-config.yaml`:

- ✅ `"main@1d777fe9b25240f0bd02de90b012c514309c6e63"` - Will be updated
- ❌ `"release-v0.15"` - Will be skipped  
- ❌ `"stable"` - Will be skipped

### Update Frequency

- **Scheduled runs**: Daily at 6:00 AM UTC
- **Manual triggers**: Available via GitHub Actions UI

## Checking imageOverrides

`make resolve-image-digests` fails if an `imageOverrides` row has an illegal key (must start with `RELATED_IMAGE_`), or if `component`, `tagTemplate`, `paramsEnvKey`, or `source` is set to a value that does not match its rules, and it does not rewrite `manifests-config.yaml`. PRs that change that YAML or `cmd/manifest-tools` run the unit tests that cover these errors.
