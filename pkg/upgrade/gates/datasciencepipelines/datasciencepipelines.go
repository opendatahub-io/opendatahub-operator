package datasciencepipelines

import (
	"context"
	"fmt"
	"slices"
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	dspaCRDName             = "datasciencepipelinesapplications.datasciencepipelinesapplications.opendatahub.io"
	deprecatedStoredVersion = "v1alpha1"
	routeAPIGroup           = "route.openshift.io"
	routeResource           = "routes"
	dspaAPIGroup            = "datasciencepipelinesapplications.opendatahub.io"
	dspaAPIResource         = "datasciencepipelinesapplications/api"
)

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	blocking := &UpgradeBlockedError{}

	storedVersion, err := validateStoredVersion(ctx, reader)
	if err != nil {
		return err
	}

	blocking.StoredVersion = storedVersion

	rolesMissingAPISubresource, err := validateRoles(ctx, reader)
	if err != nil {
		return err
	}

	blocking.RolesMissingAPISubresource = rolesMissingAPISubresource

	if blocking.StoredVersion == "" && len(blocking.RolesMissingAPISubresource) == 0 {
		return nil
	}

	return blocking
}

func validateStoredVersion(ctx context.Context, reader client.Reader) (string, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := reader.Get(ctx, client.ObjectKey{Name: dspaCRDName}, crd)
	switch {
	case k8serr.IsNotFound(err):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("getting DataSciencePipelinesApplication CRD: %w", err)
	case slices.Contains(crd.Status.StoredVersions, deprecatedStoredVersion):
		return deprecatedStoredVersion, nil
	default:
		return "", nil
	}
}

func validateRoles(ctx context.Context, reader client.Reader) ([]string, error) {
	var roles rbacv1.RoleList
	if err := reader.List(ctx, &roles); err != nil {
		return nil, fmt.Errorf("listing Roles for DataSciencePipelines RBAC migration check: %w", err)
	}

	blocking := make([]string, 0, len(roles.Items))
	for i := range roles.Items {
		role := &roles.Items[i]
		if !roleNeedsDSPAPIMigration(role) {
			continue
		}

		blocking = append(blocking, client.ObjectKeyFromObject(role).String())
	}

	sort.Strings(blocking)
	return blocking, nil
}

func roleNeedsDSPAPIMigration(role *rbacv1.Role) bool {
	requiredVerbs := verbsForResource(role.Rules, routeAPIGroup, routeResource)
	if len(requiredVerbs) == 0 {
		return false
	}

	return !ruleCoversResourceVerbs(role.Rules, dspaAPIGroup, dspaAPIResource, requiredVerbs)
}

func verbsForResource(rules []rbacv1.PolicyRule, apiGroup string, resource string) []string {
	verbsSet := make(map[string]struct{})
	for _, rule := range rules {
		if !matchesAPIGroup(rule.APIGroups, apiGroup) || !matchesResource(rule.Resources, resource) {
			continue
		}
		for _, verb := range rule.Verbs {
			verbsSet[verb] = struct{}{}
		}
	}
	if len(verbsSet) == 0 {
		return nil
	}

	verbs := make([]string, 0, len(verbsSet))
	for verb := range verbsSet {
		verbs = append(verbs, verb)
	}
	sort.Strings(verbs)

	return verbs
}

func ruleCoversResourceVerbs(rules []rbacv1.PolicyRule, apiGroup string, resource string, requiredVerbs []string) bool {
	for _, rule := range rules {
		if !matchesAPIGroup(rule.APIGroups, apiGroup) || !matchesResource(rule.Resources, resource) {
			continue
		}
		if coversVerbs(rule.Verbs, requiredVerbs) {
			return true
		}
	}

	return false
}

func matchesAPIGroup(values []string, expected string) bool {
	return slices.Contains(values, "*") || slices.Contains(values, expected)
}

func matchesResource(values []string, expected string) bool {
	return slices.Contains(values, "*") || slices.Contains(values, expected)
}

func coversVerbs(values []string, required []string) bool {
	if slices.Contains(values, "*") {
		return true
	}
	for _, verb := range required {
		if !slices.Contains(values, verb) {
			return false
		}
	}

	return true
}
