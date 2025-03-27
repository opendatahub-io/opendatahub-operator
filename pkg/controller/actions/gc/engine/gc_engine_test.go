package engine_test

import (
	"context"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	"github.com/rs/xid"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	labels2 "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc/engine"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

//nolint:gochecknoinits
func init() {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
}

func allVerb() []string {
	return []string{"delete", "create", "get", "list", "patch"}
}

func anyRule() authorizationv1.ResourceRule {
	return authorizationv1.ResourceRule{
		Verbs:     []string{engine.AnyVerb},
		APIGroups: []string{engine.AnyVerb},
		Resources: []string{engine.AnyVerb},
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
				APIGroups: []string{engine.AnyResource},
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
				engine.MatchRule(
					test.resourceGroup,
					test.apiResource,
					test.rule,
				),
			).To(test.matcher)
		})
	}
}
func TestEngine(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	ctx := context.Background()
	cli := envTest.Client()
	cmName := "gc-cm"
	cmLabels := map[string]string{"foo": "bar"}

	tests := []struct {
		name         string
		options      []engine.RunOptionsFn
		matcher      gTypes.GomegaMatcher
		countMatcher gTypes.GomegaMatcher
	}{
		{
			name:         "should not collect leftovers without an object predicate",
			matcher:      Not(HaveOccurred()),
			countMatcher: BeNumerically("==", 0),
		},
		{
			name:         "should collect leftovers matching the selector and object predicate",
			matcher:      MatchError(k8serr.IsNotFound, "IsNotFound"),
			countMatcher: BeNumerically("==", 1),
			options: []engine.RunOptionsFn{
				engine.WithSelector(labels2.SelectorFromSet(cmLabels)),
				engine.WithObjectFilter(func(_ context.Context, obj unstructured.Unstructured) (bool, error) {
					return obj.GetName() == cmName, nil
				}),
			},
		},
		{
			name:         "should not collect leftovers not matching the selector and object predicate",
			matcher:      Not(HaveOccurred()),
			countMatcher: BeNumerically("==", 0),
			options: []engine.RunOptionsFn{
				engine.WithSelector(labels2.SelectorFromSet(map[string]string{
					"foo": xid.New().String(),
				})),
				engine.WithObjectFilter(func(_ context.Context, obj unstructured.Unstructured) (bool, error) {
					return obj.GetName() == cmName, nil
				}),
			},
		},
		{
			name:         "should not collect leftovers not matching the object predicate",
			matcher:      Not(HaveOccurred()),
			countMatcher: BeNumerically("==", 0),
			options: []engine.RunOptionsFn{
				engine.WithSelector(labels2.SelectorFromSet(cmLabels)),
				engine.WithObjectFilter(func(_ context.Context, obj unstructured.Unstructured) (bool, error) {
					return obj.GetName() != cmName, nil
				}),
			},
		},
		{
			name:         "should not collect leftovers not matching the type predicate",
			matcher:      Not(HaveOccurred()),
			countMatcher: BeNumerically("==", 0),
			options: []engine.RunOptionsFn{
				engine.WithTypeFilter(func(_ context.Context, kind schema.GroupVersionKind) (bool, error) {
					return kind.Kind == "foo", nil
				}),
				engine.WithObjectFilter(func(_ context.Context, obj unstructured.Unstructured) (bool, error) {
					return obj.GetName() == cmName, nil
				}),
			},
		},
		{
			name:         "should collect leftovers matching the type and object predicate",
			matcher:      MatchError(k8serr.IsNotFound, "IsNotFound"),
			countMatcher: BeNumerically("==", 1),
			options: []engine.RunOptionsFn{
				engine.WithTypeFilter(func(_ context.Context, kind schema.GroupVersionKind) (bool, error) {
					return kind == gvk.ConfigMap, nil
				}),
				engine.WithObjectFilter(func(_ context.Context, obj unstructured.Unstructured) (bool, error) {
					return obj.GetName() == cmName, nil
				}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			id := xid.New().String()

			gci := engine.New(
				// This is required as there are no kubernetes controller running
				// with the envtest, hence we can't use the foreground deletion
				// policy (default)
				engine.WithDeletePropagationPolicy(metav1.DeletePropagationBackground),
			)

			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: id,
				},
			}

			g.Expect(cli.Create(ctx, &ns)).
				NotTo(HaveOccurred())
			g.Expect(gci.Refresh(ctx, cli, ns.Name)).
				NotTo(HaveOccurred())

			t.Cleanup(func() {
				g.Eventually(func() error {
					return cli.Delete(ctx, &ns)
				}).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			})

			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: ns.Name,
					Labels:    cmLabels,
				},
			}

			t.Cleanup(func() {
				g.Expect(cli.Delete(ctx, &cm)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			})

			g.Expect(cli.Create(ctx, &cm)).
				NotTo(HaveOccurred())

			count, err := gci.Run(ctx, cli, tt.options...)
			g.Expect(err).
				NotTo(HaveOccurred())

			if tt.matcher != nil {
				err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cm), &corev1.ConfigMap{})
				g.Expect(err).To(tt.matcher)
			}

			if tt.countMatcher != nil {
				g.Expect(count).Should(tt.countMatcher)
			}
		})
	}
}
