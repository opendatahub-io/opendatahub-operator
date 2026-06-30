package rules

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/opendatahub-io/operator-actions-framework/resources"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	VerbDelete  = "delete"
	VerbAny     = "*"
	ResourceAny = "*"
)

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
		return nil, fmt.Errorf("failed to create SelfSubjectRulesReview for namespace '%s': %w", ns, err)
	}

	if rulesReview.Status.EvaluationError != "" {
		logf.FromContext(ctx).Info("error occurred during rule evaluation: " + rulesReview.Status.EvaluationError)
	}

	return rulesReview.Status.ResourceRules, nil
}

func IsResourceMatchingRule(
	resourceGroup string,
	apiRes metav1.APIResource,
	rule authorizationv1.ResourceRule,
) bool {
	for _, ruleGroup := range rule.APIGroups {
		if resourceGroup != ruleGroup && ruleGroup != ResourceAny {
			continue
		}

		for _, ruleResource := range rule.Resources {
			if apiRes.Name == ruleResource || ruleResource == ResourceAny {
				return true
			}
		}
	}

	return false
}

func HasPermissions(
	group string,
	apiRes metav1.APIResource,
	permissionRules []authorizationv1.ResourceRule,
	requiredVerbs []string,
) bool {
	if len(requiredVerbs) == 0 {
		return false
	}

	for _, requiredVerb := range requiredVerbs {
		if !slices.ContainsFunc(permissionRules, func(rule authorizationv1.ResourceRule) bool {
			return (slices.Contains(rule.Verbs, requiredVerb) || slices.Contains(rule.Verbs, VerbAny)) &&
				IsResourceMatchingRule(group, apiRes, rule)
		}) {
			return false
		}
	}

	return true
}

func ComputeAuthorizedResources(
	resourceLists []*metav1.APIResourceList,
	rules []authorizationv1.ResourceRule,
	requiredVerbs []string,
) ([]resources.Resource, error) {
	allowedResources := make(map[resources.Resource]struct{})

	for _, list := range resourceLists {
		groupVersion, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			return nil, fmt.Errorf("unable to parse group version '%s': %w", list.GroupVersion, err)
		}

		for _, apiResource := range list.APIResources {
			group := apiResource.Group
			if group == "" {
				group = groupVersion.Group
			}

			if !HasPermissions(group, apiResource, rules, requiredVerbs) {
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

	result := slices.AppendSeq(make([]resources.Resource, 0, len(allowedResources)), maps.Keys(allowedResources))
	slices.SortFunc(result, func(a, b resources.Resource) int {
		return strings.Compare(a.String(), b.String())
	})

	return result, nil
}

func ListAuthorizedResources(
	ctx context.Context,
	cli client.Client,
	apis []*metav1.APIResourceList,
	namespace string,
	requiredVerbs []string,
) ([]resources.Resource, error) {
	apiResourceLists := discovery.FilteredBy(
		discovery.SupportsAllVerbs{
			Verbs: requiredVerbs,
		},
		apis,
	)

	items, err := RetrieveSelfSubjectRules(ctx, cli, namespace)
	if err != nil {
		return nil, fmt.Errorf("failure retrieving resource rules: %w", err)
	}

	result, err := ComputeAuthorizedResources(apiResourceLists, items, requiredVerbs)
	if err != nil {
		return nil, fmt.Errorf("failure retrieving authorized resources: %w", err)
	}

	return result, nil
}
