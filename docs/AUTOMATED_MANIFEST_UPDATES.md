# Automated Manifest Updates

This document describes the automated systems for updating OpenDataHub component manifest references.

## Overview

The `manifests-config.yaml` file contains references to various OpenDataHub component repositories and pinned ODH/RHOAI Build-Config repositories.
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
- ✅ Updates pinned Build-Config commits together with component commits

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

The command fetches the release CSV from each platform's pinned `buildConfig` commit. ODH entries resolve from the ODH CSV and may use downloaded `params.env` values. RHOAI entries do not use those ODH defaults; they resolve from the RHOAI CSV. RHOAI production references under `registry.redhat.io/rhoai/` are stored using the pullable `quay.io/rhoai/` mirror with the same digest.

For `source: csv` entries, existing platform images are refreshed, platforms no longer published are removed, and newly published platforms are added when their image registry matches the import allowlist. Images listed under `odh_exceptions` or `rhoai_exceptions` in `component-params-env.yaml` are not automatically imported for that platform. An existing platform mapping remains explicitly managed until it is removed from `imageOverrides`.

## Syncing digest-pinned Go code defaults

Some Go code falls back to a digest-pinned image when the matching `RELATED_IMAGE_*` environment variable is unset (for example the kube-auth-proxy fallback in `internal/controller/services/gateway/gateway_support.go`). When such a default must stay identical to the ODH image pinned in `manifests-config.yaml`, it is tagged with a marker comment:

```go
// sync-digests: RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE
return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:..."
```

After resolving digests, `make resolve-image-digests` rewrites the first string literal in the statement following each `sync-digests:` marker with the ODH `base@digest` configured for that env var. The pairing does not cross a statement boundary: a marker whose statement ends without a string literal (for example before a `return someFunc()` call) fails the sync instead of grabbing an unrelated digest-pinned literal from a later declaration.

Scanning starts in the directory containing `manifests-config.yaml` (override with `--code-defaults-root`). The root must be a real directory: a symlinked root is rejected with an error telling you to pass the directory it points at, and symlinked files found inside the scanned tree are skipped. An unreadable Go file fails the sync before `manifests-config.yaml` is saved — its markers cannot be validated, and silently skipping it could let a code default drift — so fix the file permissions and rerun rather than looking for a way to skip the scan. Only `.go` files are scanned, and `_test.go` files, `.git/`, `vendor/`, `testdata/`, and `opt/` are skipped. Markers are validated against the freshly resolved config **before** `manifests-config.yaml` is saved, so a bad marker (an env var missing from `imageOverrides`, no valid digest-pinned literal after it, a marker field that is not an uppercase `RELATED_IMAGE_*` name such as `RELATED_IMAGE_PROXY_amd64`, or an override that does not resolve to a valid `base@digest` container reference) fails the command without rewriting the config. A failure while writing the Go files themselves (for example a full disk) can still leave `manifests-config.yaml` already updated; rerunning the command re-syncs the code defaults. Failing loudly is deliberate: silently letting a code default drift would break the drift-guard unit tests comparing the fallback against `manifests-config.yaml`.
