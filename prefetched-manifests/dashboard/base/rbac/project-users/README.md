# RBAC - Project Users

Aggregating to Admin / Edit (aka Contributor) / View are *NOT* a Dashboard functionality.

Things in here are tech debt, or we are officially breaking into owning CRDs and their full CR lifecycle (highly unlikely at time of writing).

Read more on k8s docs: https://kubernetes.io/docs/reference/access-authn-authz/rbac/#aggregated-clusterroles

Basically, contextually, our app does not aggregate because we don't own CRDs. Our /crd folder is an overreaching construct of legacy needs. We don't watch these resources, and we don't apply them to users.

*Most of all, we do not do user-project (aka AI project) level resources.*

If you're in need of this folder, make sure you get a signoff from a Dashboard Staff+ eng.
