package rules_test

import (
	"context"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/rules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func allVerb() []string {
	return []string{"delete", "create", "get", "list", "patch"}
}

func anyRule() authorizationv1.ResourceRule {
	return authorizationv1.ResourceRule{
		Verbs:     []string{rules.VerbAny},
		APIGroups: []string{rules.VerbAny},
		Resources: []string{rules.VerbAny},
	}
}

func TestMatchRules(t *testing.T) {
	tests := []struct {
		name          string
		resourceGroup string
		apiResource   metav1.APIResource
		rule          authorizationv1.ResourceRule
		matcher       gTypes.GomegaMatcher
	}{
		//
		// Positive Match
		//

		{
			name:          "Should match",
			resourceGroup: "",
			apiResource: metav1.APIResource{
				Verbs: allVerb(),
			},
			rule:    anyRule(),
			matcher: BeTrue(),
		},
		{
			name:          "Should match as resource is explicitly listed",
			resourceGroup: "unknown",
			apiResource: metav1.APIResource{
				Name: "baz",
			},
			rule: authorizationv1.ResourceRule{
				APIGroups: []string{rules.ResourceAny},
				Resources: []string{"baz"},
			},
			matcher: BeTrue(),
		},

		//
		// Negative Match
		//

		{
			name:          "Should not match as API group is not listed",
			resourceGroup: "unknown",
			apiResource:   metav1.APIResource{},
			rule: authorizationv1.ResourceRule{
				APIGroups: []string{"baz"},
			},
			matcher: BeFalse(),
		},
		{
			name:          "Should not match as resource is not listed",
			resourceGroup: "unknown",
			apiResource:   metav1.APIResource{},
			rule: authorizationv1.ResourceRule{
				Resources: []string{"baz"},
			},
			matcher: BeFalse(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(
				rules.IsResourceMatchingRule(
					test.resourceGroup,
					test.apiResource,
					test.rule,
				),
			).To(test.matcher)
		})
	}
}

func newTestResource(group string, version string, kind string, resource string) resources.Resource {
	return resources.Resource{
		RESTMapping: meta.RESTMapping{
			GroupVersionKind: schema.GroupVersionKind{
				Group:   group,
				Version: version,
				Kind:    kind,
			},
			Resource: schema.GroupVersionResource{
				Group:    group,
				Version:  version,
				Resource: resource,
			},
			Scope: meta.RESTScopeNamespace,
		},
	}
}

func TestListAuthorizedDeletableResources(t *testing.T) {
	const testNamespace = "test-namespace"

	testCases := []struct {
		name             string
		apis             []*metav1.APIResourceList
		rules            []authorizationv1.ResourceRule
		resourcesMatcher gTypes.GomegaMatcher
	}{
		{
			name: "successful retrieval of deletable resources",
			apis: []*metav1.APIResourceList{{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{Name: "pods", Namespaced: true, Kind: "Pod", Version: "v1", Verbs: []string{"delete", "list"}},
				},
			}, {
				GroupVersion: "apps/v1",
				APIResources: []metav1.APIResource{
					{Name: "deployments", Namespaced: true, Kind: "Deployment", Group: "apps", Version: "v1", Verbs: []string{"delete", "list"}},
				},
			}},
			rules: []authorizationv1.ResourceRule{{
				Verbs:     []string{"delete"},
				APIGroups: []string{"", "apps"},
				Resources: []string{"pods", "deployments"},
			}},
			resourcesMatcher: And(
				HaveLen(2),
				ContainElements(
					newTestResource("", "v1", "Pod", "pods"),
					newTestResource("apps", "v1", "Deployment", "deployments"),
				),
			),
		},
		{
			name: "successful filter of deletable resources",
			apis: []*metav1.APIResourceList{{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{Name: "pods", Namespaced: true, Kind: "Pod", Version: "v1", Verbs: []string{"delete", "list"}},
				},
			}, {
				GroupVersion: "apps/v1",
				APIResources: []metav1.APIResource{
					{Name: "deployments", Namespaced: true, Kind: "Deployment", Group: "apps", Version: "v1", Verbs: []string{"delete", "list"}},
				},
			}},
			rules: []authorizationv1.ResourceRule{{
				Verbs:     []string{"delete"},
				APIGroups: []string{""},
				Resources: []string{"pods"},
			}},
			resourcesMatcher: And(
				HaveLen(1),
				ContainElements(
					newTestResource("", "v1", "Pod", "pods"),
				),
			),
		},
		{
			name: "no deletable resources by rule",
			apis: []*metav1.APIResourceList{{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{Name: "pods", Namespaced: true, Kind: "Pod", Verbs: []string{"delete", "list"}},
				},
			}},
			rules: []authorizationv1.ResourceRule{{
				Verbs:     []string{"list", "get"},
				APIGroups: []string{""},
				Resources: []string{"pods"},
			}},
			resourcesMatcher: BeEmpty(),
		},
		{
			name: "no deletable resources",
			apis: []*metav1.APIResourceList{{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{Name: "pods", Namespaced: true, Kind: "Pod", Verbs: []string{"list"}},
				},
			}},
			rules: []authorizationv1.ResourceRule{{
				Verbs:     []string{"delete", "list", "get"},
				APIGroups: []string{""},
				Resources: []string{"pods"},
			}},
			resourcesMatcher: BeEmpty(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()

			s, err := scheme.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			mapper := meta.NewDefaultRESTMapper(s.PreferredVersionAllGroups())
			for kt := range s.AllKnownTypes() {
				switch kt {
				case gvk.CustomResourceDefinition:
					mapper.Add(kt, meta.RESTScopeRoot)
				case gvk.ClusterRole:
					mapper.Add(kt, meta.RESTScopeRoot)
				default:
					mapper.Add(kt, meta.RESTScopeNamespace)
				}
			}

			cli := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRESTMapper(mapper).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						if s, ok := obj.(*authorizationv1.SelfSubjectRulesReview); ok {
							s.Status.ResourceRules = tc.rules
							return nil
						}

						return client.Create(ctx, obj, opts...)
					},
				}).
				Build()

			items, err := rules.ListAuthorizedDeletableResources(ctx, cli, tc.apis, testNamespace)

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(items).Should(tc.resourcesMatcher)
		})
	}
}
