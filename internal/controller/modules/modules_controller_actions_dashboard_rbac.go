package modules

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	dashboardSANameODH   = "odh-dashboard"
	dashboardSANameRHOAI = "rhods-dashboard"
	dashboardRoleKind    = "Role"
	rbacVerbCreate       = "create"
	rbacVerbGet          = "get"
	rbacVerbUpdate       = "update"
	rbacVerbDelete       = "delete"
	rbacVerbList         = "list"
	rbacVerbPatch        = "patch"
	rbacResourceSecrets  = "secrets"
	rbacResourceCMs      = "configmaps"
	rbacResourcePVCs     = "persistentvolumeclaims"

	// dashboardManagedRBACLabel marks Roles and RoleBindings created by this
	// reconciler so they can be found and deleted when a namespace is no longer active.
	dashboardManagedRBACLabel      = "dashboard.opendatahub.io/managed-rbac"
	dashboardManagedRBACLabelValue = "true"
)

// dashboardRBACTarget pairs a namespace with the role suffix to apply there.
type dashboardRBACTarget struct {
	namespace string
	suffix    string
}

func ensureDashboardNamespacedRBAC(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	// Resolve active targets first (empty when dashboard is disabled).
	targets, saName, err := resolveDashboardActiveState(ctx, rr)
	if err != nil {
		return err
	}

	// Build the set of active (namespace, name) pairs for stale cleanup.
	// Keying by namespace alone would skip cleanup when two targets share a
	// namespace and one of them is removed (e.g. ModelRegistry deleted while
	// Workbenches still active in the same namespace).
	activeKeys := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		activeKeys[t.namespace+"/"+saName+"-"+t.suffix] = struct{}{}
	}

	// Always clean up stale Roles/RoleBindings before creating new ones.
	// This handles: dashboard disabled, namespace changes, ModelRegistry removal.
	if err := cleanupStaleDashboardRBAC(ctx, rr, activeKeys); err != nil {
		return err
	}

	logger := logf.FromContext(ctx)
	appNamespace := cluster.GetApplicationNamespace()
	for _, t := range targets {
		logger.V(1).Info("ensuring Dashboard RBAC", "namespace", t.namespace, "suffix", t.suffix)
		rules := dashboardRBACRulesForSuffix(t.suffix)
		if err := addDashboardNamespacedRBAC(ctx, rr, saName, appNamespace, t.namespace, t.suffix, rules); err != nil {
			return fmt.Errorf("failed to add RBAC for namespace %s: %w", t.namespace, err)
		}
	}

	return nil
}

// resolveDashboardActiveState returns the list of namespace/suffix targets that
// should have RBAC, and the SA name to use. Returns an empty slice when dashboard is disabled.
// Using a slice (not a map) preserves both entries when notebooks and model-registry resolve
// to the same namespace.
func resolveDashboardActiveState(ctx context.Context, rr *odhtype.ReconciliationRequest) ([]dashboardRBACTarget, string, error) {
	if !DefaultRegistry().IsEnabled(componentApi.DashboardComponentName) {
		return nil, "", nil
	}

	saName := dashboardSANameODH
	if rr.Release.Name == cluster.SelfManagedRhoai || rr.Release.Name == cluster.ManagedRhoai {
		saName = dashboardSANameRHOAI
	}

	var targets []dashboardRBACTarget

	notebooksNS, err := resolveDashboardNotebooksNamespace(ctx, rr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve notebooks namespace: %w", err)
	}
	if notebooksNS != "" {
		targets = append(targets, dashboardRBACTarget{namespace: notebooksNS, suffix: "notebooks"})
	}

	modelRegistryNS, err := resolveDashboardModelRegistryNamespace(ctx, rr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve model-registry namespace: %w", err)
	}
	if modelRegistryNS != "" {
		targets = append(targets, dashboardRBACTarget{namespace: modelRegistryNS, suffix: "model-registries"})
	}

	return targets, saName, nil
}

// cleanupStaleDashboardRBAC deletes labeled Roles/RoleBindings whose namespace/name
// key is no longer in the active set. Runs unconditionally so that disabling the
// dashboard, changing namespaces, or removing a component always revokes access promptly.
// activeKeys is keyed as "namespace/roleName" so that removing one target from a shared
// namespace doesn't skip cleanup of the other target's objects.
func cleanupStaleDashboardRBAC(ctx context.Context, rr *odhtype.ReconciliationRequest, activeKeys map[string]struct{}) error {
	logger := logf.FromContext(ctx)
	labelSelector := client.MatchingLabels{dashboardManagedRBACLabel: dashboardManagedRBACLabelValue}

	roleList := &rbacv1.RoleList{}
	if err := rr.Client.List(ctx, roleList, labelSelector); err != nil {
		return fmt.Errorf("listing managed dashboard Roles: %w", err)
	}
	for i := range roleList.Items {
		role := &roleList.Items[i]
		if _, active := activeKeys[role.Namespace+"/"+role.Name]; active {
			continue
		}
		logger.Info("deleting stale dashboard Role", "namespace", role.Namespace, "name", role.Name)
		if err := rr.Client.Delete(ctx, role); err != nil && !k8serr.IsNotFound(err) {
			return fmt.Errorf("deleting stale Role %s/%s: %w", role.Namespace, role.Name, err)
		}
	}

	rbList := &rbacv1.RoleBindingList{}
	if err := rr.Client.List(ctx, rbList, labelSelector); err != nil {
		return fmt.Errorf("listing managed dashboard RoleBindings: %w", err)
	}
	for i := range rbList.Items {
		rb := &rbList.Items[i]
		if _, active := activeKeys[rb.Namespace+"/"+rb.Name]; active {
			continue
		}
		logger.Info("deleting stale dashboard RoleBinding", "namespace", rb.Namespace, "name", rb.Name)
		if err := rr.Client.Delete(ctx, rb); err != nil && !k8serr.IsNotFound(err) {
			return fmt.Errorf("deleting stale RoleBinding %s/%s: %w", rb.Namespace, rb.Name, err)
		}
	}

	return nil
}

func dashboardRBACRulesForSuffix(suffix string) []rbacv1.PolicyRule {
	if suffix == "model-registries" {
		return dashboardModelRegistryRBACRules()
	}
	return dashboardNotebooksRBACRules()
}

func resolveDashboardNotebooksNamespace(ctx context.Context, rr *odhtype.ReconciliationRequest) (string, error) {
	logger := logf.FromContext(ctx)

	if !DefaultRegistry().IsEnabled(componentApi.WorkbenchesComponentName) {
		logger.V(1).Info("Workbenches not enabled, skipping notebooks RBAC")
		return "", nil
	}

	var ns string
	if dsc := dscFromInstance(rr); dsc != nil {
		ns = dsc.Spec.Components.Workbenches.WorkbenchNamespace
	}
	if ns == "" {
		switch rr.Release.Name {
		case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
			ns = cluster.DefaultNotebooksNamespaceRHOAI
		case cluster.OpenDataHub:
			ns = cluster.DefaultNotebooksNamespaceODH
		}
	}

	exists, err := cluster.NamespaceExists(ctx, rr.Client, ns)
	if err != nil {
		return "", fmt.Errorf("failed to check notebooks namespace %q: %w", ns, err)
	}
	if !exists {
		logger.V(1).Info("notebooks namespace not found, skipping RBAC", "namespace", ns)
		return "", nil
	}

	return ns, nil
}

func resolveDashboardModelRegistryNamespace(ctx context.Context, rr *odhtype.ReconciliationRequest) (string, error) {
	logger := logf.FromContext(ctx)

	mr := &componentApi.ModelRegistry{}
	err := rr.Client.Get(ctx, client.ObjectKey{Name: componentApi.ModelRegistryInstanceName}, mr)
	if err != nil {
		if k8serr.IsNotFound(err) {
			logger.V(1).Info("ModelRegistry CR not found, skipping model-registry RBAC")
			return "", nil
		}
		return "", fmt.Errorf("failed to get ModelRegistry CR: %w", err)
	}

	ns := mr.Spec.RegistriesNamespace
	if ns == "" {
		logger.V(1).Info("ModelRegistry registriesNamespace is empty, skipping RBAC")
		return "", nil
	}

	exists, err := cluster.NamespaceExists(ctx, rr.Client, ns)
	if err != nil {
		return "", fmt.Errorf("failed to check model-registry namespace %q: %w", ns, err)
	}
	if !exists {
		logger.V(1).Info("model-registry namespace not found, skipping RBAC", "namespace", ns)
		return "", nil
	}

	return ns, nil
}

func addDashboardNamespacedRBAC(ctx context.Context, rr *odhtype.ReconciliationRequest, saName, saNamespace, targetNamespace, roleSuffix string, rules []rbacv1.PolicyRule) error {
	roleName := saName + "-" + roleSuffix
	managedLabels := map[string]string{
		dashboardManagedRBACLabel: dashboardManagedRBACLabelValue,
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: targetNamespace,
			Labels:    managedLabels,
		},
		Rules: rules,
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: targetNamespace,
			Labels:    managedLabels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     dashboardRoleKind,
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      saName,
				Namespace: saNamespace,
			},
		},
	}

	// Apply directly via SSA instead of rr.AddResources: the deploy action looks
	// up resources via the informer cache, which does not cover cross-namespace
	// targets like rhods-notebooks or the model-registry namespace. SSA writes
	// go directly to the API server and bypass the cache restriction.
	fieldOwner := client.FieldOwner("odh-operator-dashboard-rbac")
	if err := resources.Apply(ctx, rr.Client, role, client.ForceOwnership, fieldOwner); err != nil {
		return fmt.Errorf("failed to apply Role %s/%s: %w", targetNamespace, roleName, err)
	}
	if err := resources.Apply(ctx, rr.Client, rb, client.ForceOwnership, fieldOwner); err != nil {
		return fmt.Errorf("failed to apply RoleBinding %s/%s: %w", targetNamespace, roleName, err)
	}
	return nil
}

func dashboardNotebooksRBACRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{rbacResourcePVCs},
			Verbs:     []string{rbacVerbCreate, rbacVerbGet},
		},
		{
			APIGroups: []string{""},
			Resources: []string{rbacResourceCMs},
			Verbs:     []string{rbacVerbCreate, rbacVerbGet, rbacVerbUpdate},
		},
		{
			APIGroups: []string{""},
			Resources: []string{rbacResourceSecrets},
			Verbs:     []string{rbacVerbCreate, rbacVerbGet, rbacVerbUpdate},
		},
	}
}

func dashboardModelRegistryRBACRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{rbacResourceSecrets},
			Verbs:     []string{rbacVerbCreate, rbacVerbDelete, rbacVerbGet, rbacVerbList, rbacVerbPatch},
		},
		{
			APIGroups: []string{""},
			Resources: []string{rbacResourceCMs},
			Verbs:     []string{rbacVerbCreate, rbacVerbList},
		},
	}
}
