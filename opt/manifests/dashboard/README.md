# Manifests

The Dashboard manifests run on Kustomize. There are 3 types of deployments for the Dashboard component.

- Open Data Hub ([`./odh`](./odh))
- Red Hat OpenShift AI
  - RHOAI Managed ([`./rhoai/addon`](./rhoai/addon))
  - RHOAI Self Managed ([`./rhoai/onprem`](./rhoai/onprem))

Each deployment type will have a `params.env` file where the Operator can inject values for us to use.

## Adding/Modifying Manifests

Rules for keeping the manifest files in a sane order:

1. When adding a new type of thing, always make it have its own folder; be sure to add the reference to the parent folder's `kustomziation.yaml` (if applicable)
2. When adding to a preexisting folder, be sure to add it to the root `kustomization.yaml` in that folder
3. Do not reference "a file" (has an extension) inside another folder. Reference other folders, which will pick up the `kustomization.yaml` inside that folder; those `kustomization.yaml` files should reference sibling files
4. Folders matter -- see the README in each for more details

## Installation (ODH)

You can use the `kustomize` tool to process the manifest for the `oc apply` command.

```markdown
# Set the namespace in the manifest where you want to deploy the dashboard
kustomize edit set namespace <DESTINATION NAMESPACE>
kustomize build common | oc apply -f -
kustomize build core-bases/base | oc apply -f -
```

Alternatively, you can use the `./install/deploy.sh` which uses the `overlays/dev` overlay to select specific folders.

## Testing Changes

One way to test changes locally is to generate the full structure before your changes, and then again after your changes.

Before:
```markdown
# Generate the files before your changes for a baseline
kustomize build rhoai/addon > before-rhoai-addon-test.yaml
kustomize build rhoai/onprem > before-rhoai-onprem-test.yaml
kustomize build odh > before-odh-test.yaml
```

After:
```markdown
# Generate the files after your changes
kustomize build rhoai/addon > after-rhoai-addon-test.yaml
kustomize build rhoai/onprem > after-rhoai-onprem-test.yaml
kustomize build odh > after-odh-test.yaml

# Generate the diff between the two
git diff --no-index before-rhoai-addon-test.yaml after-rhoai-addon-test.yaml > output-rhoai-addon.diff
git diff --no-index before-rhoai-onprem-test.yaml after-rhoai-onprem-test.yaml > output-rhoai-onprem.diff
git diff --no-index before-odh-test.yaml after-odh-test.yaml > output-odh.diff
```

Viewing the diffs will help you understand what changed.
