## Upgrade testing 
Follow below step for manual upgrade testing 

1. Set environment variables to overwrite values in Makefile. You should overwrite the IMAGE_OWNER and VERSION etc values for pushing container images into your quay.io account.

```
IMAGE_OWNER ?= IMAGE_OWNER
VERSION ?= VERSION
```

2. Add `replaces` property in [opendatahub-operator.clusterserviceversion.yaml](https://github.com/opendatahub-io/opendatahub-operator/blob/114a137d6289c748d421e7560f6f4fdf925e1b1f/config/manifests/bases/opendatahub-operator.clusterserviceversion.yaml) and add version which you would like to upgrade with next version

```
replaces: opendatahub-operator.v2.4.0
```

3. Build and push docker container image

```
make image
```

4. Build bundle image 

```
make bundle-build
```

5. Push bundle image into registry

```
make bundle-push
```

6. Build catalog source image 

```
make catalog-build
```

7. Push catalog source image into registry

```
make catalog-push
```
### Cluster
Deploy CatalogSource on cluster to install `Open Data Hub Operator`.


8. Deploy CatalogSource. Deploy catalog source on cluster by using your catalog source container image and wait until catalog source pod is ready

```console
cat <<EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: opendatahub-catalog
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: quay.io/$IMAGE_OWNER/opendatahub-operator-catalog:$VERSION
  displayName: Open Data Hub Operator
  publisher: ODH
EOF
```

9. Subscribe to the ODH custom catalog

```console
cat <<EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: opendatahub-operator
  namespace: openshift-operators
spec:
  channel: fast
  name: opendatahub-operator
  source: opendatahub-catalog
  sourceNamespace: openshift-marketplace
EOF
```

### Upgrade to new Version

1. Follow steps `1 to 7` to build new version of catalog-source image

2. Apply updated version of catalog

```console
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: opendatahub-catalog
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: quay.io/$IMAGE_OWNER/opendatahub-operator-catalog:$VERSION
  displayName: Open Data Hub Operator
  publisher: ODH
EOF
```

3. Select `fast` channel to update

4. Go to Installed Operator `Open Data Hub Operator` and upgrade operator with latest version under tab `Subscription`

### Usage

1. When Operator is installed it creates a namespace called `opendatahub`.
2. Users need to create required `DataScienceCluster` resource by going to the `Installed Operators` tab in the OpenShift Cluster.
3. Users should explicitly set components with `managementState: Managed` in order for components to be installed.

```console
cat <<EOF | oc apply -f -
apiVersion: datasciencecluster.opendatahub.io/v1
kind: DataScienceCluster
metadata:
  name: example
spec:
  components:
    codeflare:
      managementState: Managed
    dashboard:
      managementState: Managed
    datasciencepipelines:
      managementState: Managed
    kserve:
      managementState: Managed
      serving:
        ingressGateway:
          certificate:
            type: OpenshiftDefaultIngress
        managementState: Managed
        name: knative-serving
    modelmeshserving:
      managementState: Managed
    modelregistry:
      managementState: Removed
      registriesNamespace: "rhoai-model-registries"
    ray:
      managementState: Managed
    kueue:
      managementState: Managed
    trainingoperator:
      managementState: Managed
    workbenches:
      managementState: Managed
    trustyai:
      managementState: Managed
    feastoperator:
      managementState: Managed
      
EOF
```