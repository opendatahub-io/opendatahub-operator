# What

This document serves as the knowledge base for troubleshooting the Open Data Hub Operator.

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

### How build troubleshooting my operator with debug mode

In order to have step by step debug mode on, these need to be followed:

- ensure manifests exists locally if not run `make get-manifests`

- build a silverbullet image and push it to quay.io
  `make silverbullet-image`

- deploy silverbullet image into your cluster
  `make silverbullet-deploy`

- enable breakpoint in your IDE(i.e VSCode)