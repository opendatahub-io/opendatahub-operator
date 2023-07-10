# Open Data Hub Operator : Dev Preview

ODH Operator is introducing new CRD called DataScienceCluster. The new feature set will be
released in phases and will be made available before release in the form of a `custom` Operator catalog

## Deploying Custom Catalog

1. ODH Operator team will provide new catalogsource image with tag corresponding to latest `pre-release` in ODH [releases](https://github.com/opendatahub-io/opendatahub-operator/releases).

   Alternatively, you can directly get the preview version -
   ```console
   export RELEASE_TAG=$( curl https://api.github.com/repos/opendatahub-io/opendatahub-operator/releases | jq -r 'map(select(.prerelease)) | first | .tag_name')
   ```
2. Deploy CatalogSource

   ```console
   $ cat <<EOF | oc create -f -
    apiVersion: operators.coreos.com/v1alpha1
    kind: CatalogSource
    metadata:
    name: opendatahub-dev-catalog
    namespace: openshift-marketplace
    spec:
    sourceType: grpc
    image: quay.io/opendatahub/opendatahub-operator-catalog:$RELEASE_TAG
    displayName: Open Data Hub Operator (Preview)
    publisher: ODH
    EOF

   ```
3. Subscribe to the ODH custom catalog
   ```console
   $ cat <<EOF | oc create -f -
    apiVersion: operators.coreos.com/v1alpha1
    kind: Subscription
    metadata:
    name: opendatahub-operator
    namespace: openshift-operators
    spec:
    channel: fast
    name: opendatahub-operator
    source: opendatahub-dev-catalog
    sourceNamespace: openshift-marketplace
    EOF
   ```

## Usage

1. When Operator is installed it creates a namespace called `opendatahub`.
2. Users need to create required `DataScienceCluster` resource by going to the `Installed Operators` tab in the OpenShift Cluster.
3. Install components by setting up `profile` field or setting individual components as `enabled`.

### Integrated Components

- Currently on integration of ODH [core](https://opendatahub.io/docs/tiered-components/) components is available with the Operator. 
- Tier 1 and Tier 2 components can be deployed manually using [kustomize build](https://kubectl.docs.kubernetes.io/references/kustomize/cmd/build/)
