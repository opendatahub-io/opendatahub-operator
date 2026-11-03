# CRD storage version migration

After manager leadership, the operator runs three independent best-effort storage
migration attempts for DataScienceCluster (DSC), DSCInitialization (DSCI), and
Platform. Each target uses its installed CRD's current storage version. This
runs separately from normal bootstrap and reconciliation, including when DSC or
DSCI controllers are suppressed. It does not block readiness. With leader
election disabled, each runnable executes once in the single manager.

A missing CRD or valid storage history already containing only its storage
version produces a Normal `StorageVersionMigrationNotNeeded` event and stops
before API discovery or request creation for that CRD. The other CRDs are still
processed independently. A later CRD installation or storage version change is
evaluated at the next operator startup.

The attempt follows four steps: validate, discover, run, and cleanup. Each
executed step reports one outcome event. Successful steps use these Normal
reasons, in order:

- `StorageVersionMigrationValidated`
- `StorageVersionMigrationDiscovered`
- `StorageVersionMigrationSucceeded` (the migration and history patch succeeded)
- `StorageVersionMigrationCleanedUp`

A failed step emits a Warning with reason `StorageVersionMigrationFailed` and
the step's action. Later steps for that CRD do not execute; the other migrations
continue independently. A cleanup failure can therefore follow a successful
migration event without undoing the completed pruning. Manager cancellation
stops the running attempts.

The cluster must provide a functioning storage migration API **and controller**.
The operator prefers `storagemigration.k8s.io/v1`, then `v1beta1`, then
`v1alpha1`, then `migration.k8s.io/v1alpha1`. The first supported API is selected
once and used for the entire attempt. Requests in other API groups are not
inspected or resumed, including after an API preference change on restart. It
does not install a migrator or rewrite custom resources itself. Each CRD attempt
is limited to five minutes.

Requests are bound to the CRD UID, generation, and target storage version. A
storage version change followed by a rollback therefore creates a fresh request
instead of reusing an earlier success.

Only a successful migration request permits a fresh, resourceVersion-guarded
patch of the CRD's `status.storedVersions`. A changed CRD generation stops
pruning; a concurrent change after the safety read rejects the patch with a
conflict. In either case, the request remains for inspection and recovery.
The operator deletes its successful request after pruning. If cleanup fails,
pruning is still complete; a later startup is a no-op and does not remove the
leftover request. If migration is needed for a later CRD generation, it uses a
new request and leaves older requests untouched.

## Diagnosis and recovery

Failures are logged and leave the manager running. A best-effort Warning event
with reason `StorageVersionMigrationFailed` refers to the CRD; standard
`events.k8s.io` events for this cluster-scoped object appear in `default`:

```sh
oc get crd datascienceclusters.datasciencecluster.opendatahub.io -o yaml
oc get events.events.k8s.io -n default
oc get events.events.k8s.io -n default --field-selector reason=StorageVersionMigrationFailed
kubectl get storageversionmigrations.storagemigration.k8s.io
kubectl get storageversionmigrations.migration.k8s.io
```

List all storage migration requests in the API group selected by kubectl:

```sh
kubectl get storageversionmigrations
```

Requests are cluster-scoped. If both API groups are served, use the qualified
commands above to inspect each group. Successful requests are deleted after
storage history pruning; their outcomes are recorded as events.

List all events for the DSC CRD in creation order:

```sh
kubectl get events -n default \
  --field-selector involvedObject.name=datascienceclusters.datasciencecluster.opendatahub.io \
  --sort-by=.metadata.creationTimestamp
```

For DSCI or Platform, replace the name with
`dscinitializations.dscinitialization.opendatahub.io` or
`platforms.config.opendatahub.io`, respectively. Watch those events as the
operator runs:

```sh
kubectl get events -n default \
  --field-selector involvedObject.name=datascienceclusters.datasciencecluster.opendatahub.io \
  --watch
```

An unavailable API group can make the corresponding inspection command fail.
Events for a missing CRD reference its known name and kind without a UID; the
operator does not create a placeholder CRD. Event delivery uses the standard
asynchronous recorder and is best effort.

Use the request name/API in the error and inspect its spec, ownership/stage
annotations, and conditions. A matching pending or successful request can resume
on restart only for the same CRD generation. A Failed request for that generation
is retained and never automatically retried: review the failure, repair its cause,
and manually delete that request before restarting. An operator upgrade that
changes the CRD spec increases its generation and creates a fresh request if
migration is needed; changing only the operator image does not. Requests from
before generation tracking was introduced are left untouched and are not resumed.
Mismatched requests require review; the operator never adopts unmarked or
CLI-created requests. Manager shutdown cancellation emits no warning.

Startup migration cannot rescue an upgrade whose CRD installation is already
blocked by removing a stored version. Use the odh-cli pre-upgrade storage
migration before that upgrade. The operator's simulated migration tests verify
orchestration, concurrency, and authorization, not actual etcd rewriting.
