# Using custom manifests with OLM operator

Use custom manifests for development with an ODH operator deployed by OLM, by mounting a volume and copying the manifests into it.

## Not for production use

This is a development convenience flow, and is not at all supported in any production setting.

## Pre-requisites

- [x] ODH operator deployed by OLM and running in the cluster
- [x] Operator is in a "Succeeded" state in OLM

## Steps

### Create a PersistentVolumeClaim

Note that the example PVC in `pvc.yaml` here is called `my-component-manifests`.
Consider naming that for your component, and creating one for each component that you need custom manifests for.

``` shell
# namespace here should be wherever operator is deployed
oc create -f pvc.yaml -n openshift-operators
```

### Patch ClusterServiceVersion

This uses the PVC name, so if you've changed that, then change it in the patch file also.

The example in the `component-dev-csv-patch.json` file mounts the volume to the dashboard manifests location at `/opt/manifests/dashboard`.
Update as appropriate for your component.

Note also that the CSV is a namespaced resource that is duplicated into every namespace for some reason, so you need to make sure that you specify the correct namespace for the operator here, so that it's patching the correct one:

``` shell
# Change to correct name and namespace for CSV
CSV=$(oc get csv -n openshift-operators -o name | grep opendatahub-operator | head -n1 | cut -d/ -f2)
oc patch csv "$CSV" -n openshift-operators --type json --patch-file csv-patch.json
```

### Wait for operator pod readiness

Use appropriate label and namespace for ODH or RHOAI or whatever your version is:

``` shell
oc wait --for='jsonpath={.status.conditions[?(@.type=="Ready")].status}=True' po -l name=opendatahub-operator -n openshift-operators
```

### Copy manifests into volume, through pod

From the relevant component repo (or wherever your custom manifests are stored), use `oc cp` to copy into the pod at the volume path.
This is done in a loop in this dashboard example, since the same structure is desired in the manifests location in the container, so we don't want to have an extra "manifests" path part.

``` bash
oc cp manifests/. $(oc get po -l name=opendatahub-operator -n openshift-operators -o jsonpath="{.items[0].metadata.namespace}/{.items[0].metadata.name}"):/opt/manifests/dashboard
```

### Restart operator pod to pick up on new manifests

``` shell
oc rollout restart deploy -n openshift-operators -l name=opendatahub-operator
```

## Explanation of csv-patch.json

The csv-patch.json is unfortunately not idempotent yet, the experience around this flow will hopefully improve over time.
It patches the CSV, which will cause the operator Deployment to get updated appropriately.

### Set replicas to 1

This is needed because usually the PVC will be RWO, meaning that it can't be mounted to multiple pods.

### Set "recreate" deployment strategy

This is needed because usually the PVC will be RWO, meaning that it can't be mounted to multiple pods (and there would be multiple for a short period of time with the default rolling strategy).

### Add an fsGroup to pod security context

This is needed for the container user uid to be able to write to the volume (for `oc cp`)

### Add volumeMount to container

This specifies which volume to mount, and at which path.
The "name" value here should match the name of the volume that's specified in the "volumes" section.
The "mountPath" should be set to the location that the operator expects to read manifests for this component.

### Add volume to pod template

This is where the PVC name is specified for the Pod.
The "name" here should match the "name" of the volume in the container's "volumeMount".
