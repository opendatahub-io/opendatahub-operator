# What

This document serves as the knowledge base for troubleshooting the Open Data Hub Operator.
More information can be found at https://github.com/opendatahub-io/opendatahub-operator/wiki

## Troubleshooting

### Upgrade from Operator v2.0/v2.1 to v2.2+

This also applies to any local build deployment from the "main" branch.

To upgrade, follow these steps:

- Disable the component(s) in your DSC instance.
- Delete both the DSC instance and DSCI instance.
- Click "uninstall" Open Data Hub operator.
- If exposed on v1alpha1, delete the DSC CRD and DSCI CRD.

All of the above steps can be performed either through the console UI or via the `oc`/`kubectl` CLI.
After completing these steps, please refer to the installation guide to proceed with a clean installation of the v2.2+ operator.


### Why component's managementState is set to {} not Removed?

Only if managementState is explicitliy set to "Managed" on component level, below configs in DSC CR to component "X" take the same effects:

```console
spec:
components:
    X:
        managementState: Removed

```

```console
spec:
components:
    X: {}
```

