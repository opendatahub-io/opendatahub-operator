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
)

func ensureDashboardNamespacedRBAC(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if !DefaultRegistry().IsEnabled(componentApi.DashboardComponentName) {
		return nil
	}

	logger := logf.FromContext(ctx)

	saName := dashboardSANameODH
	if rr.Release.Name == cluster.SelfManagedRhoai || rr.Release.Name == cluster.ManagedRhoai {
		saName = dashboardSANameRHOAI
	}

	appNamespace := cluster.GetApplicationNamespace()

	notebooksNS, err := resolveDashboardNotebooksNamespace(ctx, rr)
	if err != nil {
		return fmt.Errorf("failed to resolve notebooks namespace: %w", err)
	}
	if notebooksNS != "" {
		logger.V(1).Info("ensuring Dashboard RBAC in notebooks namespace", "namespace", notebooksNS)
		if err := addDashboardNamespacedRBAC(ctx, rr, saName, appNamespace, notebooksNS, "notebooks", dashboardNotebooksRBACRules()); err != nil {
			return fmt.Errorf("failed to add notebooks RBAC resources: %w", err)
		}
	}

	modelRegistryNS, err := resolveDashboardModelRegistryNamespace(ctx, rr)
	if err != nil {
		return fmt.Errorf("failed to resolve model-registry namespace: %w", err)
	}
	if modelRegistryNS != "" {
		logger.V(1).Info("ensuring Dashboard RBAC in model-registry namespace", "namespace", modelRegistryNS)
		if err := addDashboardNamespacedRBAC(ctx, rr, saName, appNamespace, modelRegistryNS, "model-registries", dashboardModelRegistryRBACRules()); err != nil {
			return fmt.Errorf("failed to add model-registry RBAC resources: %w", err)
		}
	}

	return nil
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

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: targetNamespace,
		},
		Rules: rules,
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: targetNamespace,
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
