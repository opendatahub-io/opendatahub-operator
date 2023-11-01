# Dashboard

The Open Data Hub Dashboard component installs a UI which:

- Shows what's installed
- Show's what's available for installation
- Links to component UIs
- Links to component documentation

For more information, visit the project [GitHub repo](https://github.com/opendatahub-io/odh-dashboard).

## Folders

1. base: contains all the necessary yaml files to install the dashboard
1. manifests/overlays/odhdashboardconfig: **OPTIONAL** overlay to deploy an ODHDashboardConfig with the odh-dashboard application.  This is only required if you want to deploy configuration outside of the default ODHDashboardConfigs that will be initialized at runtime

### Installation with KFDef

You can deploy the dashboard using the [odh-dashboard-kfnbc-test.yaml](odh-dashboard-kfnbc-test.yaml)

```yaml
  - kustomizeConfig:
      repoRef:
        name: manifests
        path: odh-dashboard
    name: odh-dashboard
```

If you would like to deploy the default configs for the Dashboard groups and `ODHDashboardConfig` you can enable the `odhdashboardconfig` overlay.
NOTE: If you deploy this with the odh-operator, you will need to allow the operator to deploy the initial version of the files and then remove the `odhdashboardconfig` from the overlay to prevent the operator from reseting any changes made to the groups or config

```yaml
  - kustomizeConfig:
      overlays:
        - odhdashboardconfig
      repoRef:
        name: manifests
        path: odh-dashboard
    name: odh-dashboard
```

If you would want to test the incubation version of the dashboard, you can enable the `incubation` overlay.

```yaml
  - kustomizeConfig:
      overlays:
        - incubation
      repoRef:
        name: manifests
        path: manifests
    name: odh-dashboard
```
