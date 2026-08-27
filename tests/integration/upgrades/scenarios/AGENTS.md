# Upgrade gate scenarios

This directory contains reproducible, intentionally offending resources for
the 2.x to 3.5.1 upgrade-gate integration tests. These resources are not
expected to become Ready or serve traffic. They only need to satisfy the
upgrade-gate predicates.

## Preconditions

Use an OpenShift/CRC cluster with the old RHOAI 2.25 operator installed first.
The baseline must have:

- `DSC/default-dsc` and `DSCI/default-dsci` in the `redhat-ods-*` namespaces;
- release status `2.25.x` on both resources;
- CodeFlare, DataSciencePipelines, KServe, Kueue, ModelMeshServing, Ray,
  TrustyAI, and Workbenches managed in the DSC;
- the CRDs supplied by those components installed.

The exact baseline used while creating these fixtures is documented in
`SETUP.md`.

Stop and remove the old RHOAI operator's Subscription and CSV before starting
the checked-out operator. Preserve the DSC, DSCI, CRDs, and operand resources.
For this repository checkout, run the current operator with:

```bash
GOCACHE=/tmp/odh-gocache OPERATOR_NAMESPACE="redhat-ods-operator" \
  APPLICATIONS_NAMESPACE="redhat-ods-applications" \
  ODH_PLATFORM_TYPE=SelfManagedRHOAI RHAI_DISABLE_GATEWAY_SERVICE=true \
  make run-nowebhook
```

## Apply scenarios

Apply the ordinary fixtures from the repository root:

```bash
kubectl apply -f tests/integration/upgrades/scenarios/resources/00-namespace.yaml
kubectl apply -f tests/integration/upgrades/scenarios/resources/00-codeflare.yaml
kubectl apply -f tests/integration/upgrades/scenarios/resources/01-modelmeshserving.yaml
kubectl apply -f tests/integration/upgrades/scenarios/resources/02-kserve.yaml
kubectl apply -f tests/integration/upgrades/scenarios/resources/03-trustyai.yaml
kubectl apply -f tests/integration/upgrades/scenarios/resources/04-ray.yaml
kubectl apply -f tests/integration/upgrades/scenarios/resources/05-workbench-prerequisites.yaml
kubectl apply -f tests/integration/upgrades/scenarios/resources/06-workbench-notebook.yaml
kubectl delete -f tests/integration/upgrades/scenarios/resources/05-workbench-prerequisites.yaml
kubectl patch crd datasciencepipelinesapplications.datasciencepipelinesapplications.opendatahub.io \
  --subresource=status --type=merge \
  --patch-file tests/integration/upgrades/scenarios/resources/07-dsp-stored-versions.json
```

The temporary Workbench HardwareProfile and Secret must exist during Notebook
creation because the installed admission webhooks validate both references.
They are deleted immediately afterward, leaving the Notebook with broken
references.

The KServe fixture in `02-kserve.yaml` also has the Kueue queue label and is in
the intentionally unlabeled `gate-scenarios` namespace. This exercises the
Kueue workload/namespace gate in addition to the KServe gates.

## Observe the result

After the current operator reconciles, inspect the gate ConfigMap and DSC:

```bash
kubectl get configmap odh-upgrade-acks -n redhat-ods-operator -o yaml
kubectl get dsc default-dsc -o jsonpath='{.status.release.version}{"\n"}'
kubectl get dsc default-dsc -o yaml | sed -n '/conditions:/,$p'
```

Expected unacknowledged categories include CodeFlare, ModelMeshServing,
KServe, Ray, TrustyAI, Workbenches, DSP stored versions, and Kueue. The
Service Mesh dependency can also block because the baseline Subscription uses
the `stable` channel. Cert-manager is installed in the documented baseline,
so its dependency gate should not block.

## Operational lessons

- The DSC and DSCI CRDs serve both `v1` and `v2`, with `v2` as the storage
  version. Do not patch an object's `apiVersion` field. Read or update the
  existing object through `datasciencecluster.opendatahub.io/v2` or
  `dscinitialization.opendatahub.io/v2`; conversion preserves the UID and the
  2.25 release status.
- The v2 DSC API no longer has `codeflare` or `modelmeshserving` component
  fields. Their standalone `default-codeflare` and
  `default-modelmeshserving` resources are still checked by the removal gates.
- The exact DSP CRD is
  `datasciencepipelinesapplications.datasciencepipelinesapplications.opendatahub.io`.
  Its `status.storedVersions` is a status-subresource field; the supplied JSON
  patch intentionally leaves only `v1`.
- Kueue's dependency gate reads the generated `default-kueue` CR, but the DSC
  is its source of truth. Change `spec.components.kueue.managementState` on
  `default-dsc` first. For the missing-operator case use `Unmanaged` and make
  sure there is no `kueue-operator` Subscription.
- Admission webhooks reject deliberately broken objects at creation time.
  KServe InferenceServices need a predictor, ServingRuntimes need a container,
  TrustyAIService needs metrics, RayCluster needs a structural head/worker
  spec, and Notebook references must initially exist. Create temporary valid
  dependencies, create the fixture, and then delete those dependencies.
- Deleting a fixture may not trigger gate evaluation. Force a harmless DSC
  reconcile if the ConfigMap remains stale:

  ```bash
  kubectl patch dsc default-dsc --type=merge \
    -p '{"metadata":{"annotations":{"upgrade-gate-test/reconcile":"updated"}}}'
  ```

- Gate entries retain old blocker text until reconciliation. Verify that the
  value is exactly `"true"` instead of inferring success from deletion alone.

## Cleanup

Remove only the scenario fixtures when returning to a clean baseline:

```bash
kubectl delete -f tests/integration/upgrades/scenarios/resources/00-codeflare.yaml --ignore-not-found
kubectl delete -f tests/integration/upgrades/scenarios/resources/01-modelmeshserving.yaml --ignore-not-found
kubectl delete -f tests/integration/upgrades/scenarios/resources/02-kserve.yaml --ignore-not-found
kubectl delete -f tests/integration/upgrades/scenarios/resources/03-trustyai.yaml --ignore-not-found
kubectl delete -f tests/integration/upgrades/scenarios/resources/04-ray.yaml --ignore-not-found
kubectl delete -f tests/integration/upgrades/scenarios/resources/06-workbench-notebook.yaml --ignore-not-found
kubectl delete -f tests/integration/upgrades/scenarios/resources/00-namespace.yaml --ignore-not-found
```

Do not delete CRDs as part of scenario cleanup. If the old operator is still
running, it may recreate the default CodeFlare and ModelMeshServing objects.
