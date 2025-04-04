package rules

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/exp/maps"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	// VerbDelete represents the Kubernetes delete permission verb.
	VerbDelete = "delete"

	// VerbAny represents a wildcard for any permission verb.
	VerbAny = "*"

	// ResourceAny represents a wildcard for any resource.
	ResourceAny = "*"
)

// RetrieveSelfSubjectRules retrieves the list of resource rules for the current subject
// (user or service account) in the specified namespace. It creates a SelfSubjectRulesReview
// to determine what actions the current subject is allowed to perform within the namespace.
//
// Parameters:
//   - ctx: The context for the client operation
//   - cli: The Kubernetes client interface used to create the SelfSubjectRulesReview
//   - ns: The namespace for which to retrieve the subject's resource rules
//
// Returns:
//   - []authorizationv1.ResourceRule: A slice of ResourceRule objects describing what actions
//     the current subject can perform on which resources in the specified namespace
//   - error: nil on success, or an error with context if the operation fails
//
// The function will return an error if:
//   - Creating the SelfSubjectRulesReview fails
//   - The SelfSubjectRulesReview returns an evaluation error
func RetrieveSelfSubjectRules(
	ctx context.Context,
	cli client.Client,
	ns string,
) ([]authorizationv1.ResourceRule, error) {
	rulesReview := authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{
			Namespace: ns,
		},
	}

	err := cli.Create(ctx, &rulesReview)
	if err != nil {
		return nil, fmt.Errorf("unable to create SelfSubjectRulesReviews: %w", err)
	}

	if rulesReview.Status.EvaluationError != "" {
		return nil, fmt.Errorf("error occurred during rule evaluation: %s", rulesReview.Status.EvaluationError)
	}

	return rulesReview.Status.ResourceRules, nil
}

// IsResourceMatchingRule determines if a Kubernetes API resource matches an authorization rule.
// It checks whether the resource's group and name are covered by the permissions defined in the
// rule.
//
// Parameters:
//   - resourceGroup: The API group of the resource being checked
//   - apiRes: The API resource metadata
//   - rule: The authorization rule to match against
//
// Returns:
//   - bool: true if the resource matches the rule, false otherwise
func IsResourceMatchingRule(
	resourceGroup string,
	apiRes metav1.APIResource,
	rule authorizationv1.ResourceRule,
) bool {
	// Check if the resource group matches any of the rule's API groups
	for _, ruleGroup := range rule.APIGroups {
		// Skip if the group doesn't match and isn't a wildcard
		if resourceGroup != ruleGroup && ruleGroup != ResourceAny {
			continue
		}

		// Check if the resource name matches any of the rule's resources
		for _, ruleResource := range rule.Resources {
			if apiRes.Name == ruleResource || ruleResource == ResourceAny {
				return true
			}
		}
	}

	return false
}

// HasDeletePermission checks if the current subject has permission to delete a specific API
// resource based on the provided authorization rules.
//
// Parameters:
//   - group: The API group of the resource to check
//   - apiRes: The API resource metadata
//   - permissionRules: A slice of authorization rules to check against
//
// Returns:
//   - bool: true if the resource can be deleted by the current subject, false otherwise
func HasDeletePermission(
	group string,
	apiRes metav1.APIResource,
	permissionRules []authorizationv1.ResourceRule,
) bool {
	for _, rule := range permissionRules {
		// Skip if the rule doesn't grant delete permission
		if !slices.Contains(rule.Verbs, VerbDelete) && !slices.Contains(rule.Verbs, VerbAny) {
			continue
		}

		// Skip if the resource doesn't match this rule
		if !IsResourceMatchingRule(group, apiRes, rule) {
			continue
		}

		return true
	}

	return false
}

// ComputeDeletableResources returns a sorted list of Kubernetes resources that the user
// is authorized to delete, based on the available API resources and RBAC rules.
//
// Parameters:
//   - resourceLists: A slice of metav1.APIResourceList. Each entry describes a group/version
//     and the set of API resources (kinds) available under it.
//   - rules: A slice of authorizationv1.ResourceRule, representing the set of actions
//     the user is allowed to perform. These typically come from a SubjectAccessReview
//     or SelfSubjectRulesReview.
//
// Return values:
//   - []resources.Resource: A slice of resources the user can delete. Each resource includes
//     its GroupVersionResource, GroupVersionKind, and scope (namespaced or cluster-wide).
//     The slice is sorted by the string representation of the resource.
//   - error: Returned if any GroupVersion string in the API resource list fails to parse.
func ComputeDeletableResources(
	resourceLists []*metav1.APIResourceList,
	rules []authorizationv1.ResourceRule,
) ([]resources.Resource, error) {
	allowedResources := make(map[resources.Resource]struct{})

	for _, list := range resourceLists {
		groupVersion, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			return nil, fmt.Errorf("unable to parse group version: %w", err)
		}

		for _, apiResource := range list.APIResources {
			group := apiResource.Group
			if group == "" {
				group = groupVersion.Group
			}

			if !HasDeletePermission(group, apiResource, rules) {
				continue
			}

			resource := resources.Resource{
				RESTMapping: meta.RESTMapping{
					Resource: schema.GroupVersionResource{
						Group:    groupVersion.Group,
						Version:  groupVersion.Version,
						Resource: apiResource.Name,
					},
					GroupVersionKind: schema.GroupVersionKind{
						Group:   groupVersion.Group,
						Version: groupVersion.Version,
						Kind:    apiResource.Kind,
					},
					Scope: meta.RESTScopeNamespace,
				},
			}

			if apiResource.Namespaced {
				resource.Scope = meta.RESTScopeNamespace
			} else {
				resource.Scope = meta.RESTScopeRoot
			}

			allowedResources[resource] = struct{}{}
		}
	}

	result := maps.Keys(allowedResources)
	slices.SortFunc(result, func(a, b resources.Resource) int {
		return strings.Compare(a.String(), b.String())
	})

	return result, nil
}

// ListAuthorizedDeletableResources returns a list of Kubernetes resources that the current
// service account is authorized to delete within the specified namespace.
//
// It uses the discovery API to filter for resources that support the "delete" verb and
// cross-references this list with the user's effective RBAC permissions.
//
// This prevents querying resources that do not support deletion or that the user does not have
// permission to delete, thereby avoiding unnecessary or unauthorized API calls (e.g., errors
// like "MethodNotAllowed").
//
// Parameters:
//   - ctx: A context used for request cancellation and timeouts.
//   - cli: A controller-runtime client used to make Kubernetes API calls.
//   - apis: A slice of APIResourceList, typically returned by discovery from the API server.
//   - namespace: The namespace in which to evaluate the user's permissions.
//
// Returns:
//   - []resources.Resource: A slice of deletable resources the user is authorized to delete,
//     including their GVK, GVR, and scope information.
//   - error: Non-nil if permission checks or deletable resource computation fails.
func ListAuthorizedDeletableResources(
	ctx context.Context,
	cli client.Client,
	apis []*metav1.APIResourceList,
	namespace string,
) ([]resources.Resource, error) {
	// We only take types that support the "delete" verb,
	// to prevents from performing queries that we know are going to
	// return "MethodNotAllowed".
	apiResourceLists := discovery.FilteredBy(
		discovery.SupportsAllVerbs{
			Verbs: []string{VerbDelete},
		},
		apis,
	)

	// Get the permissions of the service account in the specified namespace.
	items, err := RetrieveSelfSubjectRules(ctx, cli, namespace)
	if err != nil {
		return nil, fmt.Errorf("failure retrieving resource rules: %w", err)
	}

	// Collect deletable resources.
	result, err := ComputeDeletableResources(apiResourceLists, items)
	if err != nil {
		return nil, fmt.Errorf("failure retrieving deletable resources: %w", err)
	}

	return result, nil
}
