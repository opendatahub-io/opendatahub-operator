# Manifests

The Dashboard manifests run on Kustomize. There are now 2 types of deployments for the Dashboard component.

- Open Data Hub ([`./odh`](./odh))
- Red Hat OpenShift AI, aka RHOAI Self Managed ([`./rhoai`](./rhoai))

Each deployment type will have a `params.env` file where the Operator can inject values for us to use. For documentation of parameters that are injected as environment variables (e.g. `gateway-domain` → `GATEWAY_DOMAIN`), see [Dashboard environment variables](../docs/dashboard-environment-variables.md).

## The "How" Of Manifests

A common inquiry from the developers working on the Dashboard repo is "how" do these manifests impact installations and what happens during the varying states the given Dashboard component on a cluster goes through; eg. Upgrades, fresh installations, and how does it all work.

A couple key points to get out of the way first:
1. There are multiple "types" of manifest files (specifics can be found in [this ADR](https://github.com/opendatahub-io/architecture-decision-records/blob/main/architecture-decision-records/operator/ODH-ADR-Operator-0008-resources-lifecycle.md))
    * **Unmanaged** (Rare) -- An "install once" mentality; gets onto a cluster but after that it's a user-resource (never to be managed by us again)
    * **Fully Managed** (Very Common) -- Part of the ecosystem of the Dashboard; upgrades, user-changes, and effectively anything you do to the resources on the cluster should result in the Operator setting it back to what is in the manifest file -- this is the most desired state for most of our manifests
    * **Partially Managed** (Rare) -- Some resources (like the [Dashboard Deployment](./base/deployment.yaml)) can have some fields modified (like replica count or resource requests/limits) but the rest falls under the managed state -- this is handled as internal Operator logic and not something we typically have any control over
2. The entirety of the `manifests` folder is not installed _ever_ -- a subset of it is based on which "deployment type" you choose

With those said, the key takeaways from this section are:
* The `manifests` folder speaks more to how we show up on a cluster -- not how we update with it
* There are some **Unmanaged** resources that need to be treated very softly as once a customer has it installed, we need external help to address it
    * Dashboard team does not have a mechanism inside our wheelhouse to update an **Unmanaged** file; the Operator team has an upgrade script that needs to be involved if we have critical changes needed
    * The [RHOAI OdhDashboardConfig](rhoai/odhdashboardconfig/README.md) is the most common friction point
* Currently, the Dashboard has no mechanism to create or manage partially managed resources - this functionality is controlled by the Operator. The Dashboard can only support and interact with such resources as defined by the Operator's management model.

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
kustomize build odh | oc apply -f -
```

For standalone mode (BFF modules as independent pods managed by the dashboard-operator):

```markdown
kustomize build odh/standalone | oc apply -f -
```

## Testing Changes

One way to test changes locally is to generate the full structure before your changes, and then again after your changes. Do this from the `/manifests` folder to make use of the git-ignore file.

Before:
```markdown
# Generate the files before your changes for a baseline
kustomize build rhoai > before-rhoai-test.yaml
kustomize build odh > before-odh-test.yaml
```

After:
```markdown
# Generate the files after your changes
kustomize build rhoai > after-rhoai-test.yaml
kustomize build odh > after-odh-test.yaml

# Generate the diff between the two
git diff --no-index before-rhoai-test.yaml after-rhoai-test.yaml > output-rhoai.diff
git diff --no-index before-odh-test.yaml after-odh-test.yaml > output-odh.diff
```

Viewing the diffs will help you understand what changed.

## Automated Validation

The repository includes a GitHub Actions workflow (`.github/workflows/validate-kustomize.yml`) that automatically validates manifest changes using Kustomize. This workflow:

- **Triggers automatically** on pushes and pull requests that modify files in the `manifests/` directory
- **Validates all deployment types** in parallel:
  - RHOAI (`manifests/rhoai`)
  - ODH (`manifests/odh`)

This validation ensures that any manifest changes don't break the Kustomize build process before they're merged. If you see validation failures in CI, you can run the same `kustomize build` commands locally to debug the issue.
