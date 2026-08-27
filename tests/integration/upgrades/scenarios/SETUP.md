# CRC upgrade-gate setup

This records the disposable CRC environment used to exercise the upgrade
gates from a realistic 2.x installation.

## Cluster

- Context: `crc-admin`
- Platform: OpenShift `4.22.7`
- Applications namespace: `redhat-ods-applications`
- Workbenches namespace: `rhods-notebooks`
- Operator namespace: `redhat-ods-operator`
- Monitoring namespace: `redhat-ods-monitoring`

## Installed operators and dependencies

All listed CSVs were `Succeeded` when the fixtures were prepared:

| Operator/dependency | Namespace | Version/channel |
| --- | --- | --- |
| RHOAI operator | `redhat-ods-operator` | `2.25.8`, `stable-2.25` |
| cert-manager operator | `cert-manager-operator` | `1.20.0`, `stable-v1` |
| Service Mesh operator | `openshift-operators` | `2.6.17`, `stable` |
| Serverless operator | `openshift-serverless` | `1.37.1`, `stable` |

The RHOAI 2.25 operator supplied the component operands and CRDs, including
KServe `v0.14.0`, ModelMesh Serving `v0.12.0`, Kueue `v0.11.6`, KubeRay
`1.4.0`, CodeFlare `1.15.0`, TrustyAI `v1.37.0`, Kubeflow Pipelines `2.5.0`,
Kubeflow Notebook Controller `1.10.0`, and Kubeflow Trainer `1.9.0`.

Authorino was intentionally not installed. This makes the LLMInferenceService
scenario exercise the KServe Authorino/TLS readiness blocker.

The Kueue operator was later installed as `kueue-operator.v1.4.1` in
`openshift-kueue-operator`. The dependency scenario was tested first with
`default-dsc.spec.components.kueue.managementState=Unmanaged` and no Kueue
Subscription, then with that Subscription installed; the gate changed from a
blocker to `"true"` as expected.

## DSCI configuration

Reference object: `resources/baseline-dsci.yaml`.

- Name: `default-dsci`
- Release status: `OpenShift AI Self-Managed 2.25.8`
- Applications namespace: `redhat-ods-applications`
- Service Mesh: `Managed`
- Service Mesh control plane: `data-science-smcp` in `istio-system`
- Monitoring: `Removed`
- Trusted CA bundle: `Removed`

## DSC configuration

Reference object: `resources/baseline-dsc.yaml`.

Managed components:

```text
aipipelines, kserve, kueue, ray, trainingoperator, trustyai, workbenches
```

CodeFlare and ModelMeshServing are legacy v1 component resources and are not
fields in the v2 DSC API. Their standalone `default-codeflare` and
`default-modelmeshserving` CRs remain installed specifically for the removal
gate scenarios.

Removed components were Dashboard, FeastOperator, LlamaStackOperator, and
ModelRegistry. KServe used Serverless mode with the `knative-serving` ingress
name; Kueue used `default` for both default queue names; Workbenches used
`rhods-notebooks`.

## Gate prerequisites

The current operator creates and evaluates `odh-upgrade-acks` only when it
detects a deployed release with major version `2`. A fresh 3.x installation
will create an empty/no-op gate ConfigMap instead. Keep the 2.25 status in
place until all scenarios have been observed.

## Reconciliation notes

The checked-out operator converted the live DSC and DSCI objects to their v2
served representation while retaining release `2.25.8`. The gate ConfigMap is
created in `redhat-ods-operator` and refreshed during DSC/module
reconciliation. After changing or deleting a fixture, force a harmless DSC
metadata reconcile if the ConfigMap still contains the old blocker message.

The final run cleared the gates in sequence: KServe and Kueue workload gates
after removing their fixtures, Kueue dependency after installing its operator,
CodeFlare and ModelMeshServing after deleting their legacy CRs, Ray and
TrustyAI after repairing their state, Workbenches after deleting its broken
Notebook, and DSP after setting the CRD stored version to `v1`.
