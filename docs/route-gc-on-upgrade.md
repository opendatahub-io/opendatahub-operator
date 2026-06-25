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

### RHOAI 2.x — One route, one URL

The Dashboard controller creates a single OpenShift Route:

```
https://rhods-dashboard-redhat-ods-applications.apps.<cluster>/  →  Dashboard service  →  Dashboard pod
```

This route lives in redhat-ods-applications and is owned by the Dashboard CR. Note that the 2.x operator did not stamp platform.opendatahub.io/\* annotations on resources — that stamping framework was introduced in the 3.x reconciler refactor (December 2024)(Stamping flow is explained in GC part). The route does have standard OpenShift annotations (e.g., haproxy.router.openshift.io/timeout), which is relevant for how GC handles it during upgrades (see below).

### RHOAI 3.3 — Gateway replaces per-component routing

A central Gateway API takes over. Instead of each component having its own route, one Gateway route handles all traffic:

```
https://data-science-gateway.apps.<cluster>/  →  Gateway service  →  All RHOAI components
```

The old rhods-dashboard route is gone — automatically deleted by GC (explained below). No redirect exists. The old URL simply stops working.

### RHOAI 3.4+ — New URL with backward-compatible redirects

The gateway hostname changes from data-science-gateway to rh-ai, and two redirect routes are created for backward compatibility:

```
Old Dashboard URL:  https://rhods-dashboard-<ns>.apps.<cluster>/  ──301──▶ https://rh-ai.apps.<cluster>/
Old Gateway URL:    https://data-science-gateway.apps.<cluster>/  ──301──▶ https://rh-ai.apps.<cluster>/
```

Both redirects are powered by an Nginx deployment that returns 301 Moved Permanently, preserving the original request path so deep links also redirect correctly. For example, https://rhods-dashboard-.../projects/myproject redirects to https://rh-ai.apps.\<cluster\>/projects/myproject.

---

# How Does GC Delete Old Routes?

## Annotation Stamping Framework

The current annotation-stamping and GC framework was introduced in the 3.x reconciler refactor (December 2024). Before that, the 2.x operator deployed resources using a legacy code path (pkg/deploy/deploy.go). The legacy path used Server-Side Apply (SSA), but it did not stamp platform.opendatahub.io/\* annotations on resources.

This means:

* 2.x resources (like the old rhods-dashboard route) do not carry platform annotations. They have other annotations (OpenShift-specific ones, component labels, etc.), but no platform.opendatahub.io/version or similar stamps.  
* 3.x resources carry the full set of platform annotations, stamped by the deploy action on every reconciliation.

This difference matters for understanding how GC decides what to delete — it handles both cases.

### Step 1: Garbage Collection (GC) Workflow

GC is the last action in every controller's reconciliation pipeline. But it does not run every time. The first check is:

Did the controller render and deploy any resources in this reconciliation cycle?

If nothing was rendered (no changes), GC skips entirely. This avoids expensive cluster-wide listing on every reconciliation loop. The flag rr.Generated is set to true by the render/kustomize/helm actions when they produce resources.

### Step 2: GC only looks at resources with the right label

GC does not scan every resource in the cluster. It uses a label selector to find only resources that belong to its controller:

```
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

platform.opendatahub.io/version: "3.4.0"            \# operator version  
platform.opendatahub.io/type: "OpenShift AI ..."     \# platform type  
platform.opendatahub.io/instance.uid: "5c6238b6..."  \# owning CR's UID  
platform.opendatahub.io/instance.generation: "2"     \# owning CR's generation

The decision tree:

```
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

Since 2.x resources do NOT carry platform annotations (the stamping framework didn't exist yet), they fall into the "has annotations but missing platform stamps" case. OpenShift routes always have some annotations (e.g., haproxy.router.openshift.io/timeout), so GetAnnotations() is not nil. But the four platform.opendatahub.io/\* stamps are all missing. GC treats this as "not stamped by the current deploy framework" and deletes the resource.

However, the label and ownership checks (steps 2–3) still apply. A 2.x resource can only be GC'd if it also has the right platform.opendatahub.io/part-of label and the right ownerReference. The 3.x deploy action stamps these on resources during the first reconciliation after upgrade, including on resources that the 3.x controller adopts from the 2.x era.

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

Step-by-step upgrade process from 2.x to 3.3:

1. The 2.x operator had a rhods-dashboard route in redhat-ods-applications. This route has OpenShift annotations but no platform.opendatahub.io/\* stamps (2.x didn't stamp them).  
2. The 3.3 operator starts. The DSC controller creates the Dashboard CR.  
3. The Dashboard controller reconciles for the first time.  
4. Render — In 3.3, the Dashboard controller no longer renders an OpenShift Route (routing moved to HTTPRoute/Gateway API). Other resources (ConfigMaps, Deployments, etc.) are rendered.  
5. Deploy (SSA) — The rendered resources are applied. Each one gets stamped with version: 3.3.3, part-of: dashboard, etc. The old rhods-dashboard route is not touched because it was not rendered.  
6. GC — Lists all resources with part-of: dashboard and owned by Dashboard. The old route is found (it carries this label and owner from the 2.x era). GC checks annotations: the route has annotations (OpenShift ones) but all four platform stamps are missing → deleted.  
7. Meanwhile, the Gateway controller creates the new data-science-gateway route with part-of: gatewayconfig and owned by GatewayConfig.

---

# Validation and Testing

### Upgrade 1: 2.25.7 → 3.3.3

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
* GC found the old rhods-dashboard route (has part-of: dashboard label and Dashboard owner). The route had OpenShift annotations but no platform.opendatahub.io/\* stamps (2.x didn't stamp them) → missing stamps → deleted it  
* Gateway controller created the new data-science-gateway route (with part-of: gatewayconfig and owned by GatewayConfig)

Impact: Old URL https://rhods-dashboard-... stopped working immediately with no redirect.

### Upgrade 2: 3.3.3 → 3.4.0

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

```
$ curl -I https://rhods-dashboard-redhat-ods-applications.apps.../
HTTP/1.1 301 Moved Permanently
Location: https://rh-ai.apps.../
```

---

# Redirect Lifecycle and Removal

### Today: Explicit deletion

The redirect routes and their supporting resources (Nginx Deployment, Service, ConfigMap) are explicitly deleted when:

* Dashboard component is removed — If the Dashboard CR doesn't exist, createDashboardRedirects cleans up directly. This explicit approach is needed because removing Dashboard doesn't change GatewayConfig's generation, so GC alone would miss them.  
* Feature disabled by admin — Setting DISABLE\_DASHBOARD\_REDIRECTS=true in the operator Subscription env removes all redirect resources immediately.

### Future: 

### Phase 1 — 3.N: Redirects OFF, opt-in to keep

### The code logic flips from:

```
// 3.4: disabled only if explicitly set
if os.Getenv("DISABLE_DASHBOARD_REDIRECTS") == "true" { ... }
```

### 

### To:

```
// 3.N: enabled only if explicitly set
if os.Getenv("ENABLE_DASHBOARD_REDIRECTS") != "true" { ... }
```

###  On upgrade to 3.N:

* Redirects stop working by default — old URLs return 404/503

* Admins who still need time can re-enable via Subscription:

```
spec:
  config:
    env:
    - name: ENABLE_DASHBOARD_REDIRECTS
      value: "true"
```

* The operator logs a warning: "Dashboard redirects are deprecated and will be removed in 3.N+1. Update bookmarks to [https://rh-ai.apps](https://rh-ai.apps/)./"

* A Kubernetes Event is emitted on the GatewayConfig CR for visibility in oc describe

This gives admins one full release cycle to confirm all users have migrated, with a safe rollback path.

### Phase 2 — 3.N: GC handles it automatically

When the redirect feature is removed from the codebase in a future release (e.g., 3.N), the same GC mechanism that deleted the rhods-dashboard route in 3.3 will clean up the redirect resources:

1. Remove createDashboardRedirects action and templates from the codebase  
2. On upgrade to 3.N, the Gateway controller no longer deploys redirect resources  
3. GC finds the existing redirects with version: 3.4.0 ≠ 3.N → deletes them automatically

No special migration code needed. The only change is removing the redirect action and templates. GC does the rest — the same proven pattern from the 2.x → 3.3 upgrade.

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

### Administrators

Right now (3.4):

* Old URLs work via redirect — nothing is broken  
* Communicate to users: update bookmarks to https://rh-ai.apps.\<cluster\>/

Optional — disable redirects early on a specific cluster:

```
# In Subscription for rhods-operator  
spec:  
  config:  
    env:  
    - name: DISABLE_DASHBOARD_REDIRECTS  
      value: "true"
```

### End users

Update your bookmarks from:

```
https://rhods-dashboard-redhat-ods-applications.apps.<cluster>/
https://data-science-gateway.apps.<cluster>/
```

To the new stable URL:

```
https://rh-ai.apps.<cluster>/
```

