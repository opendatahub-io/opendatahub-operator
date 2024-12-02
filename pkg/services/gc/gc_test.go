package gc_test

import (
	"testing"

	gTypes "github.com/onsi/gomega/types"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/services/gc"

	. "github.com/onsi/gomega"
)

func allVerb() []string {
	return []string{"delete", "create", "get", "list", "patch"}
}

func anyRule() authorizationv1.ResourceRule {
	return authorizationv1.ResourceRule{
		Verbs:     []string{gc.AnyVerb},
		APIGroups: []string{gc.AnyVerb},
		Resources: []string{gc.AnyVerb},
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
				APIGroups: []string{gc.AnyResource},
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
				gc.MatchRule(
					test.resourceGroup,
					test.apiResource,
					test.rule,
				),
			).To(test.matcher)
		})
	}
}
