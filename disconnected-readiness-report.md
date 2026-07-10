# Disconnected Readiness Score

**Repository:** opendatahub-io/opendatahub-operator
**Date:** 2026-07-10
**Branch:** main

| Verdict | Rule | Summary |
|---------|------|---------|
| PASS | no-image-tags | All production fallback images use digest-pinned references |
| PASS | csv-relatedimages | 110+ `RELATED_IMAGE_*` env vars across 18 components |
| PASS | no-runtime-egress | No production runtime egress detected |
| PASS | image-manifest-complete | All manifest images have `RELATED_IMAGE_*` mappings |
| PASS | python-imports | N/A -- Go project, no Python dependencies |

**Blockers: 0 | Warnings: 1 | Passed: 5**

---

## WARNING: CSV base template uses version tag (accepted false positive)

The base CSV template at `config/manifests/bases/opendatahub-operator.clusterserviceversion.yaml`
contains `containerImage: quay.io/opendatahub/opendatahub-operator:v3.5.0` with a version tag
instead of a digest.

This is an accepted false positive. The field is overwritten at multiple stages:
1. `.github/scripts/update-versions.sh` rewrites it during release staging
2. External build configs (ODH-Build-Config / RHOAI-Build-Config) replace it with a digest-pinned image for the distributed CSV

The tagged image in the base template never reaches production. No source-level fix is possible
without breaking the multi-stage release pipeline.

## Resolved: Previously reported blockers

### Gateway `:latest` fallbacks (fixed)

The two gateway fallback images that previously used `:latest` tags are now digest-pinned:
- `gateway_support.go` `getKubeAuthProxyImage()` -- `@sha256:f9d9dc6e0e05...`
- `gateway_support.go` `getDashboardRedirectImage()` -- `@sha256:f0a79ccf21b8...`

### Monitoring Perses image tag (fixed)

The Perses fallback image previously used `tag@sha256:digest` format. The redundant tag has been
removed, now using digest-only format consistent with all other fallback images in the repo.

### Monitoring fallback images (fixed)

All monitoring fallback images (`kube-rbac-proxy`, `prom-label-proxy`, `CLI`) are now
digest-pinned in both upstream and RHOAI default paths.

### Anaconda CronJob images (fixed)

The partner CronJob at `config/partners/anaconda/base/anaconda-ce-validator-cron.yaml` previously
had two images without `RELATED_IMAGE_*` mappings. Both now have corresponding env vars
(`RELATED_IMAGE_ANACONDA_CE_CLI`, `RELATED_IMAGE_ANACONDA_CE_NOTEBOOK`) on the container spec,
making them discoverable by OLM for image mirroring. The inline script uses the env var for the
notebook image reference.

### Anaconda CronJob egress (fixed)

The CronJob's `curl` to `repo.anaconda.cloud` is inherent to its license-validation purpose.
It was already `suspend: true` by default. A `DISCONNECTED` env var guard has been added to
skip validation entirely when set to `"true"`, providing an additional safety net for
air-gapped deployments.

---

## Image manifest coverage

The operator declares **110+ unique `RELATED_IMAGE_*` environment variables** across all 18
component controllers. Every component's `imageParamMap` maps kustomize parameter names to their
corresponding `RELATED_IMAGE_*` env var. The CSV populates these env vars with digest-pinned
images at deploy time.
