# RHOAI Route Cleanup: Upgrade Mechanics

What this explains: How OpenShift routes get created, updated, and automatically deleted when you upgrade RHOAI from 2.x → 3.3 → 3.4+.

Validated: June 15, 2026 JIRA: RHOAIENG-61574

---

# Summary

| Upgrade Path | Old Dashboard URL | New URL | User Action |
| ----- | ----- | ----- | ----- |
| 2.x → 3.3 | Broken (route deleted, no redirect) | data-science-gateway.apps.\<cluster\> | Must update bookmarks |
| 2.x → 3.4+ | Works (301 redirect) | rh-ai.apps.\<cluster\> | Should update bookmarks |
| 3.3 → 3.4 | N/A | rh-ai.apps.\<cluster\> (old gateway URL redirects) | Should update bookmarks |
| 3.4 → future | Will break when redirects are removed | rh-ai.apps.\<cluster\> | Must migrate before removal |

**Bottom line:** The operator automatically cleans up old routes via garbage collection (GC). Starting from 3.4, old URLs redirect to the new rh-ai URL so nothing breaks immediately. Users should migrate bookmarks to rh-ai during this transition window.

---

# The URL Journey Across Versions

RHOAI changed its routing architecture across major versions. In 2.x, each component had its own OpenShift Route. In 3.x, a central Gateway API replaced all per-component routes with a single entry point. This means old routes need to be cleaned up during upgrades.

## RHOAI 2.x — One route, one URL

The Dashboard controller creates a single OpenShift Route:

```text
https://rhods-dashboard-redhat-ods-applications.apps.<cluster>/  →  Dashboard service  →  Dashboard pod
```

This route lives in redhat-ods-applications and is owned by the Dashboard CR. RHOAI 2.16 and earlier did not stamp platform.opendatahub.io/\* annotations on resources — the stamping framework was introduced in PRs [#1320](https://github.com/opendatahub-io/opendatahub-operator/pull/1320) (October 2024) and [#1374](https://github.com/opendatahub-io/opendatahub-operator/pull/1374) (November 2024), shipped in ODH 2.22.0 (December 2024), and is present in RHOAI from 2.19 onwards.

## RHOAI 3.3 — Gateway replaces per-component routing

A central Gateway API takes over. Instead of each component having its own route, one Gateway route handles all traffic:

```text
https://data-science-gateway.apps.<cluster>/  →  Gateway service  →  All RHOAI components
```

The old rhods-dashboard route is gone — automatically deleted by GC (explained below). No redirect exists. The old URL simply stops working.

## RHOAI 3.4+ — New URL with backward-compatible redirects

The gateway hostname changes from data-science-gateway to rh-ai, and two redirect routes are created for backward compatibility:

```text
Old Dashboard URL:  https://rhods-dashboard-redhat-ods-applications.apps.<cluster>/  ──301──▶ https://rh-ai.apps.<cluster>/
Old Gateway URL:    https://data-science-gateway.apps.<cluster>/  ──301──▶ https://rh-ai.apps.<cluster>/
```

Both redirects are powered by an Nginx deployment that returns 301 Moved Permanently, preserving the original request path so deep links also redirect correctly. For example, https://rhods-dashboard-.../projects/myproject redirects to https://rh-ai.apps.\<cluster\>/projects/myproject.

---

# How Does GC Delete Old Routes?

## Annotation Stamping Framework

The annotation-stamping and GC framework was introduced upstream in PRs [#1320](https://github.com/opendatahub-io/opendatahub-operator/pull/1320) (October 2024) and [#1374](https://github.com/opendatahub-io/opendatahub-operator/pull/1374) (November 2024), shipped in ODH 2.22.0 (December 2024). Before that, the operator deployed resources using a legacy code path (pkg/deploy/deploy.go). The legacy path used Server-Side Apply (SSA), but it did not stamp platform.opendatahub.io/\* annotations on resources.

In RHOAI terms: the stamping framework is present starting from **RHOAI 2.19**. Resources deployed by RHOAI 2.19 and later (2.21, 2.22, 2.25, etc.) carry the full set of platform annotations. **RHOAI 2.16 and earlier** use the legacy deploy path and do not stamp annotations.

This means GC handles resources differently depending on which version deployed them:

* Resources from **RHOAI 2.19+** (2.19, 2.21, 2.22, 2.25, etc.): have platform annotations and the `platform.opendatahub.io/part-of` label. GC finds them via the label selector and deletes them because the version stamp does not match the new RHOAI 3.x operator version.  
* Resources from the **current RHOAI 3.x version**: carry fresh platform annotations matching the running operator. GC keeps them.

**RHOAI 2.16 and earlier** are a special case. The legacy deploy path (`pkg/deploy/deploy.go`) set `app.kubernetes.io/part-of` (the standard Kubernetes label) but did NOT set `platform.opendatahub.io/part-of` (the label GC queries). This means GC cannot find these resources — they are invisible to the label selector. However, they are still cleaned up through other mechanisms:

* **2.x → 3.4+ upgrades:** The Gateway controller creates a redirect route with the **same name** (`rhods-dashboard`) in the **same namespace** (`redhat-ods-applications`). Server-Side Apply (SSA) overwrites the old route in-place and takes over ownership — the old route becomes the redirect route.  
* **2.x → 3.3 upgrades:** No redirect exists, and GC cannot find the route. The old `rhods-dashboard` route is **orphaned** — it remains in the cluster pointing at a Dashboard service that may no longer exist. There is no explicit upgrade code in `pkg/upgrade/` that deletes it.

### Step 1: Garbage Collection (GC) Workflow

GC is the last action in every controller's reconciliation pipeline. But it does not run every time. The first check is:

Did the controller render and deploy any resources in this reconciliation cycle?

If nothing was rendered (no changes), GC skips entirely. This avoids expensive cluster-wide listing on every reconciliation loop. The flag rr.Generated is set to true by the render/kustomize/helm actions when they produce resources.

### Step 2: GC only looks at resources with the right label

GC does not scan every resource in the cluster. It uses a label selector to find only resources that belong to its controller:

```text
platform.opendatahub.io/part-of: <controller-name>
```

Where \<controller-name\> is the lowercased kind of the CR that the controller manages.

| Controller | CR kind | GC label selector |
| ----- | ----- | ----- |
| Dashboard | Dashboard | platform.opendatahub.io/part-of: dashboard |
| Gateway | GatewayConfig | platform.opendatahub.io/part-of: gatewayconfig |
| DSC | DataScienceCluster | platform.opendatahub.io/part-of: datasciencecluster |

Any resource without this label is invisible to that controller's GC. This is the first layer of isolation between controllers — the Dashboard controller's GC never sees Gateway resources, and vice versa.

### Step 3: GC only deletes resources owned by its CR

Even after finding resources by label, GC checks each one for an ownerReference that points to the controller's CR type. By default (onlyOwned: true), GC skips any resource that is not owned by the right CR kind.

For example, even if a resource somehow had part-of: dashboard, the Dashboard controller's GC would still skip it unless it also has an ownerReference with kind: Dashboard. This is the second layer of isolation.

The ownership check uses the CR's GroupVersionKind (not a specific CR name), so it matches any instance of that CR type.

### Step 4: GC checks annotations to decide keep vs. delete

For each resource that passed both the label and ownership checks, GC reads four annotation stamps:

```text
platform.opendatahub.io/version: "3.4.0"            # operator version
platform.opendatahub.io/type: "OpenShift AI ..."     # platform type
platform.opendatahub.io/instance.uid: "5c6238b6..."  # owning CR's UID
platform.opendatahub.io/instance.generation: "2"     # owning CR's generation
```

The decision tree:

```text
Resource has NO annotations at all (GetAnnotations() == nil)
  → KEEP (not managed by operator, untouchable)

Resource has annotations but ANY platform stamp is missing
  → DELETE (resource exists but was not stamped by current deploy framework)

Resource has all four stamps, but version doesn't match current operator version
  → DELETE (stale from old version, not redeployed this cycle)

Resource has all four stamps, but platform type doesn't match
  → DELETE (wrong platform, e.g. RHOAI vs ODH)

Resource has all four stamps, but instance UID doesn't match
  → DELETE (belongs to a different CR instance)

Resource has all four stamps, but generation doesn't match
  → DELETE (CR was updated, resource was not redeployed)

All four stamps match current reconciliation state
  → KEEP (freshly deployed this cycle)
```

There are also two override rules applied before the annotation check:

* Resources marked opendatahub.io/managed: "false" are always kept (opt-out annotation).  
* Certain types (CRDs, Leases) are declared as unremovable and are always kept.

### How does this apply to 2.x routes?

The outcome depends on which 2.x version deployed the route and which 3.x version you upgrade to:

* **RHOAI 2.19+** (2.19, 2.21, 2.22, 2.25, etc.): The route carries platform annotations with the old operator version (e.g., version: "2.25.7") and has the `platform.opendatahub.io/part-of: dashboard` label. GC finds the route via the label selector, checks annotations, and deletes it because of version mismatch.

* **RHOAI 2.16 and earlier** → **3.4+**: The route does NOT carry `platform.opendatahub.io/part-of` (the legacy deploy path set `app.kubernetes.io/part-of` instead). GC cannot find it via label selector. However, the Gateway controller creates a redirect route with the **same name** (`rhods-dashboard`) in the **same namespace**, so SSA overwrites the old route in-place — the old route becomes a redirect to `rh-ai`.

* **RHOAI 2.16 and earlier** → **3.3**: GC cannot find the route (wrong label), and no redirect route is created to overwrite it. The old route is **orphaned** in the cluster. It still has the old hostname but may point at a service that no longer exists.

The label and ownership checks (steps 2–3) are important: a resource can only be GC'd if it has the right `platform.opendatahub.io/part-of` label and the right ownerReference. Resources with only `app.kubernetes.io/part-of` are invisible to GC.

### Deploy and GC happen in one reconciliation pass

Each controller runs a pipeline of actions in order during every reconciliation. The key actions are:

1. Render — Actions like createOCPRoutes and createDashboardRedirects decide which resources to create based on runtime conditions (ingress mode, whether Dashboard exists, etc.) and add Go templates to the render queue.  
2. Deploy (SSA) — Renders the templates into Kubernetes objects and applies them via Server-Side Apply. Every deployed resource gets re-stamped with the current annotations (version, generation, etc.). The deploy action also sets ownerReference and the part-of label.  
3. GC (runs last) — Lists all resources matching the controller's label and owner. Resources that were deployed in step 2 have fresh annotations → kept. Resources that were not rendered in this cycle still have old/missing annotations → deleted.

This all happens in a single reconciliation pass, not as a delayed or separate cleanup job. So when a new operator version starts up, old stale routes are deleted in the very first reconciliation — there is no delay or separate cleanup process.

### How multiple controllers avoid interfering with each other

Three layers of isolation prevent one controller's GC from deleting another controller's resources:

1. Label scope — Each controller's GC only lists resources with its own part-of value. Dashboard GC queries part-of: dashboard, Gateway GC queries part-of: gatewayconfig. They never see each other's resources.  
2. Owner reference — Even if a resource had the wrong label, GC checks that the ownerReference points to the correct CR kind. A route owned by GatewayConfig will never be deleted by the Dashboard controller's GC, because ownerReference.kind \!= Dashboard.  
3. Static type declarations — Some controllers (like DSC) further restrict GC to only delete resource types they have statically declared ownership of via .Owns(). This prevents a controller from accidentally GC-ing a resource type it doesn't manage.

### Walkthrough: 2.x → 3.x upgrade GC

Step-by-step upgrade process from 2.x to 3.3 (using RHOAI 2.25.7 → 3.3.3 as the example):

1. The 2.25.7 operator had a rhods-dashboard route in redhat-ods-applications. Since RHOAI 2.25 includes the stamping framework (based on ODH 2.22+), this route carries platform.opendatahub.io/\* annotations with version: "2.25.7".  
2. The 3.3 operator starts. The DSC controller ensures the Dashboard CR exists (creates it if missing, or adopts the existing one from 2.x).  
3. The Dashboard controller reconciles for the first time.  
4. Render — In 3.3, the Dashboard controller no longer renders an OpenShift Route (routing moved to HTTPRoute/Gateway API). Other resources (ConfigMaps, Deployments, etc.) are rendered.  
5. Deploy (SSA) — The rendered resources are applied. Each one gets stamped with version: 3.3.3, part-of: dashboard, etc. The old rhods-dashboard route is not touched because it was not rendered.  
6. GC — Lists all resources with part-of: dashboard and owned by Dashboard. The old route is found. GC checks annotations: the route has all four platform stamps, but the version ("2.25.7") does not match the current operator version ("3.3.3") → version mismatch → deleted.  
7. Meanwhile, the Gateway controller creates the new data-science-gateway route with part-of: gatewayconfig and owned by GatewayConfig.

Note: This walkthrough uses RHOAI 2.25.7 (which includes the stamping framework). For upgrades from **RHOAI 2.16 and earlier**, the route would lack both platform annotations AND the `platform.opendatahub.io/part-of` label — GC cannot find it at all. In a 2.16 → 3.3 upgrade, the old route would be orphaned. In a 2.16 → 3.4+ upgrade, the route is overwritten by SSA when the Gateway controller creates a redirect route with the same name (see "How does this apply to 2.x routes?" above).

---

# Validation and Testing

## Upgrade 1: 2.25.7 → 3.3.3

Before:

| Namespace | Route | Host |
| ----- | ----- | ----- |
| redhat-ods-applications | rhods-dashboard | rhods-dashboard-redhat-ods-applications.apps... |

After:

| Namespace | Route | Host |
| ----- | ----- | ----- |
| openshift-ingress | data-science-gateway | data-science-gateway.apps... |

What happened:

* Dashboard controller rendered its manifests — no OpenShift Route included (moved to HTTPRoute/Gateway API)  
* GC found the old rhods-dashboard route (has part-of: dashboard label and Dashboard owner). The route had platform.opendatahub.io/\* stamps from 2.25.7, but the version did not match the new operator version (3.3.3) → version mismatch → deleted it  
* Gateway controller created the new data-science-gateway route (with part-of: gatewayconfig and owned by GatewayConfig)

Impact: Old URL https://rhods-dashboard-... stopped working immediately with no redirect.

## Upgrade 2: 3.3.3 → 3.4.0

Before:

| Namespace | Route | Host |
| ----- | ----- | ----- |
| openshift-ingress | data-science-gateway | data-science-gateway.apps... |

After:

| Namespace | Route | Host | Purpose |
| ----- | ----- | ----- | ----- |
| openshift-ingress | data-science-gateway | rh-ai.apps... | Main gateway (hostname updated) |
| redhat-ods-applications | rhods-dashboard | rhods-dashboard-redhat-ods-applications.apps... | 301 redirect to rh-ai |
| redhat-ods-applications | data-science-gateway | data-science-gateway.apps... | 301 redirect to rh-ai |

What happened:

* SSA updated the existing route in-place (same route object, hostname changed to rh-ai)  
* Gateway controller created two redirect routes pointing to Nginx service  
* GC had nothing to delete (everything freshly deployed)

Impact: Old URL https://data-science-gateway.apps... works via 301 redirect. Verified with curl:

```text
$ curl -I https://rhods-dashboard-redhat-ods-applications.apps.../
HTTP/1.1 301 Moved Permanently
Location: https://rh-ai.apps.../
```

---

# Redirect Lifecycle and Removal

## Today: Explicit deletion

The redirect routes and their supporting resources (Nginx Deployment, Service, ConfigMap) are explicitly deleted when:

* Dashboard component is removed — If the Dashboard CR doesn't exist, createDashboardRedirects cleans up directly. This explicit approach is needed because removing Dashboard doesn't change GatewayConfig's generation, so GC alone would miss them.  
* Feature disabled by admin — Setting DISABLE\_DASHBOARD\_REDIRECTS=true in the operator Subscription env removes all redirect resources immediately.

## Future removal

When the redirect feature is removed from the codebase in a future release, the same GC mechanism that deleted the rhods-dashboard route in 3.3 will clean up the redirect resources:

1. Remove createDashboardRedirects action and templates from the codebase  
2. On upgrade, the Gateway controller no longer deploys redirect resources  
3. GC finds the existing redirects with a stale version annotation → deletes them automatically

No special migration code needed. The only change is removing the redirect action and templates. GC does the rest — the same proven pattern from the 2.x → 3.3 upgrade.

The exact removal timeline and mechanism will be decided as part of RHOAIENG-61576 (long-term redirect strategy recommendation).

---

# Orphan Resource Scenarios

| Scenario | What happens |
| ----- | ----- |
| Standard upgrade (2.x→3.x, 3.x→3.y) | Safe — routes cleaned up by GC or updated by SSA |
| Route created manually with platform.opendatahub.io/part-of label only | Safe — GC finds it by label, but skips it because it has no ownerReference to the controller's CR (onlyOwned check) |
| Route created manually with both the part-of label AND an ownerReference to the CR | GC will manage it — deletes it if annotations are missing or mismatched |
| Route without platform labels (e.g., model-catalog-https from model-registry-operator) | Invisible to GC, has its own lifecycle |
| Operator uninstalled without deleting CRs first | Owner references become dangling — manual cleanup needed |

---

# Administrator and User Recommendations

## Administrators

Right now (3.4):

* Old URLs work via redirect — nothing is broken  
* Communicate to users: update bookmarks to https://rh-ai.apps.\<cluster\>/

Optional — disable redirects early on a specific cluster:

```yaml
# In Subscription for rhods-operator
spec:
  config:
    env:
    - name: DISABLE_DASHBOARD_REDIRECTS
      value: "true"
```

## End users

Update your bookmarks from:

```text
https://rhods-dashboard-redhat-ods-applications.apps.<cluster>/
https://data-science-gateway.apps.<cluster>/
```

To the new stable URL:

```text
https://rh-ai.apps.<cluster>/
```

