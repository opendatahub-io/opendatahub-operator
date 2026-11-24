# DCH onboarding: pre-merge dependency validation

ODH [#4356](https://github.com/opendatahub-io/opendatahub-operator/pull/4356)
projects `spec.capabilities.dataConnectHub` into the existing `FeastOperator`.
Both required Prow E2E runs on `d8b1a42f` rejected that field because the
previously pinned Feast CRD did not declare it. The missing
`default-feastoperator` and subsequent DAG/readiness timeouts follow the
rejected apply; removing the field or weakening the tests is not a fix.

## Temporary CI inputs

Following platform-maintainer guidance, both ODH and RHOAI-mode CI use the
same published, immutable upstream dependency while Feast
[#61](https://github.com/opendatahub-io/feast-module-operator/pull/61) is reviewed:

- Chart: `opendatahub-io/feast-module-operator`,
  `HEAD@bf37e050140dbe81e99c94baa6559c20bf162aeb`, `config/chart`.
- Module image:
  `quay.io/opendatahub/odh-feast-module-operator@sha256:7299036ff265d230be170eab8f72f9f1dfcb23cc3b921475c7612086be3c25d0`.
- Successful Konflux PipelineRun:
  `open-data-hub-tenant/odh-feast-module-operator-on-pull-request-bvgr8`.
- Image `git.commit`, `vcs-ref`, and OCI revision labels match the chart SHA.
  All 122 files under the image's `/manifests` match that source byte-for-byte.
- The published image index contains Linux amd64 only. This is not validation
  of other architectures, product certification, or downstream release delivery.

`HEAD` is only the downloader's tracking label: its `branch@SHA` handling
fetches and checks out the full SHA after `@`, not the current repository HEAD.
The original `refs/pull/61/head@SHA` spelling produced two false image-tag
findings in disconnected-readiness-scorer. The equivalent `HEAD@SHA` spelling
keeps the exact source pin without resembling a registry image path. No image
digest, scanner rule, or exception is changed.

The image digests are temporarily marked `pinned` so digest resolution cannot
silently restore an incompatible older module. RHOAI-mode CI intentionally uses
this upstream PR artifact, **not** a downstream release image. Existing DCH
operand image defaults are unchanged; a passing default-install E2E run does
not establish DCH-enabled operand readiness or lifecycle correctness.

## Local validation — 2026-11-24

- Used the real manifest-tools downloader with the configured Data entries
  for each platform, then Helm linted and rendered both downloaded charts.
  Every served Feast CRD schema declares `dataConnectHub.managementState`,
  `gateway`, and `tokenReviewAudiences`; the rendered module Deployment uses
  the verified digest above.
- Ran the real `apply-deploy` image resolver against copies of both platform
  manager manifests and verified the forwarded Feast module image.
- `make generate manifests api-docs fmt lint`: passed, zero lint issues.
- `make manifests ODH_PLATFORM_TYPE=rhoai` and `make build`: passed.
  Generation left no additional tracked changes.
- `go test ./internal/controller/modules/data ./internal/controller/modules/builtin ./api/datasciencecluster/v2`:
  passed, including the eight capability combinations and omitted-DCH behavior.
- `go test ./tests/e2e -run TestAddFinalizer -count=1`: passed; this is only the
  cluster-free helper regression, not live lifecycle execution.
- `go -C cmd/manifest-tools test ./...`: passed.
- `go mod tidy`: no dependency changes.
- Disconnected scorer at CI revision
  `17c05e1d46680a47ecfc5a822ddf6f465f7baa8b`, with its unchanged central
  configuration: reproduced two `no-image-tags` blockers before the Git-ref
  spelling fix; all five rules pass with zero blockers afterward. The real
  downloader fetched byte-identical charts (15 files each) for both platforms
  with the new spelling, and both charts pass Helm lint.

Fresh Prow results are still required. These local checks do not exercise a
live cluster or resolve the separate Konflux publisher failure.

## Before merge / release

1. Preserve merge order: DCH
   [#518](https://github.com/opendatahub-io/data-connect-hub/pull/518) → Feast
   #61 → ODH #4356. Obtain the required human reviews and approvals.
2. Refresh Feast's vendored DCH manifests and matching controller/REST/Flight
   images to the accepted DCH source and successful builds before Feast merges.
3. Replace the temporary ODH chart/image pair with matching accepted upstream
   artifacts. Restore the RHOAI chart repository to
   `red-hat-data-services/feast-module-operator` on the confirmed release branch
   and use its matching downstream image. Remove these temporary `pinned`
   flags when normal release image resolution is restored.
4. Rerun both required Prow jobs on the final pins:
   `/test opendatahub-operator-e2e` and
   `/test opendatahub-operator-rhoai-e2e`. A green run on PR dependency artifacts
   is integration evidence, not permission to merge out of order.
5. Have a Konflux administrator repair the rejected shared metadata-publisher
   write credential and rerun the operator pipeline. Its successful build,
   image-index, and scan tasks do not compensate for `push-build-metadata`
   failure. No source workaround or credential value belongs in this PR.
6. Complete DCH-enabled lifecycle/upgrade/removal and disconnected validation,
   plus downstream controller-image provisioning and payload propagation.
   PostgreSQL and `dch-database-config` remain administrator prerequisites.

No live cluster changes are required to update these CI build inputs.
