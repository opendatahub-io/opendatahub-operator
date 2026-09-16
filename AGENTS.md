# Contributor Guidance

## Stable 2.x

- Stable 2.x builds RHOAI, not OpenDataHub. Use `ODH_PLATFORM_TYPE=RHOAI` and package `rhods-operator` for local workflows.
- `manifests-config.yaml` contains committed component refs and image digests.
- Do not add scheduled or per-E2E digest refresh automation. Stable branch updates should be intentional release changes.
- Refresh RHOAI image digests manually with:

  ```bash
  ODH_PLATFORM_TYPE=RHOAI make resolve-image-digests
  ```

- Review all generated `manifests-config.yaml` changes before committing.
- Normal E2E runs consume committed digests; they do not fetch latest RHOAI Build Config data.

## Checks

- Run `make lint`.
- Run `go test -C cmd/manifest-tools ./...` when manifest-tools changes.
- Run targeted E2E tests against a deployed RHOAI operator when cluster access is available.
