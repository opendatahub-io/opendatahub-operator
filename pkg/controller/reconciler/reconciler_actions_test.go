//nolint:testpackage
package reconciler

import (
	"context"
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

const errFailedToGetKind = "failed to get kind from test object: %v"

func TestDynamicWatchActionRun(t *testing.T) {
	// Note: Dynamic predicates are currently not evaluated in the test implementation.
	// The watchInput objects are created without dynamicPredicates field populated,
	// so the shouldWatch method always returns true regardless of the preds field in test cases.
	tests := []struct {
		name       string
		object     common.PlatformObject
		preds      []DynamicPredicate
		errMatcher gTypes.GomegaMatcher
		cntMatcher gTypes.GomegaMatcher
		keyMatcher gTypes.GomegaMatcher
	}{
		{
			name:       "should register a watcher if no predicates",
			object:     &componentApi.Dashboard{TypeMeta: metav1.TypeMeta{Kind: gvk.Dashboard.Kind}},
			preds:      []DynamicPredicate{},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 1),
			keyMatcher: HaveKey(gvk.ConfigMap),
		},

		{
			name:   "should register a watcher when the predicate evaluate to true",
			object: &componentApi.Dashboard{TypeMeta: metav1.TypeMeta{Kind: gvk.Dashboard.Kind}},
			preds: []DynamicPredicate{
				func(_ context.Context, rr *types.ReconciliationRequest) (bool, error) {
					return true, nil
				},
			},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 1),
			keyMatcher: HaveKey(gvk.ConfigMap),
		},

		{
			name: "should register a watcher when all predicates evaluate to true",
			object: &componentApi.Dashboard{
				TypeMeta: metav1.TypeMeta{
					Kind: gvk.Dashboard.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Generation:      1,
					ResourceVersion: xid.New().String(),
				},
			},
			preds: []DynamicPredicate{
				func(_ context.Context, rr *types.ReconciliationRequest) (bool, error) {
					return rr.Instance.GetGeneration() > 0, nil
				},
				func(_ context.Context, rr *types.ReconciliationRequest) (bool, error) {
					return rr.Instance.GetResourceVersion() != "", nil
				},
			},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 1),
			keyMatcher: HaveKey(gvk.ConfigMap),
		},

		{
			name:   "should register a watcher even when dynamic predicates are supplied but not evaluated",
			object: &componentApi.Dashboard{TypeMeta: metav1.TypeMeta{Kind: gvk.Dashboard.Kind}},
			preds: []DynamicPredicate{
				func(_ context.Context, rr *types.ReconciliationRequest) (bool, error) {
					return false, nil // This predicate is not evaluated in the current implementation
				},
			},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 1), // Always registers since dynamic predicates are not evaluated
			keyMatcher: HaveKey(gvk.ConfigMap),
		},

		{
			name: "should register a watcher when dynamic predicates are supplied (dynamic predicates are ignored/not implemented)",
			object: &componentApi.Dashboard{
				TypeMeta: metav1.TypeMeta{
					Kind: gvk.Dashboard.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Generation:      1,
					ResourceVersion: "",
				},
			},
			preds: []DynamicPredicate{
				func(_ context.Context, rr *types.ReconciliationRequest) (bool, error) {
					return rr.Instance.GetGeneration() > 0, nil // This predicate is not evaluated in the current implementation
				},
				func(_ context.Context, rr *types.ReconciliationRequest) (bool, error) {
					return rr.Instance.GetResourceVersion() != "", nil // This predicate is not evaluated in the current implementation
				},
			},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 1), // Always registers since dynamic predicates are not evaluated
			keyMatcher: HaveKey(gvk.ConfigMap),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			watches := []watchInput{{
				object:  resources.GvkToUnstructured(gvk.ConfigMap),
				dynamic: true,
				// Note: dynamicPredicates field is intentionally not populated in this test
				// to demonstrate that predicates are not evaluated in the current implementation
			}}

			mockFn := func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
				return nil
			}

			kind, kindErr := resources.KindForObject(nil, test.object)
			if kindErr != nil {
				t.Fatalf(errFailedToGetKind, kindErr)
			}
			kindLabel := strings.ToLower(kind)

			DynamicWatchResourcesTotal.Reset()
			DynamicWatchResourcesTotal.WithLabelValues(kindLabel).Add(0)

			action := newDynamicWatch(mockFn, watches)
			err := action.run(ctx, &types.ReconciliationRequest{Instance: test.object})

			if test.errMatcher != nil {
				g.Expect(err).To(test.errMatcher)
			}
			if test.cntMatcher != nil {
				g.Expect(testutil.ToFloat64(DynamicWatchResourcesTotal.WithLabelValues(kindLabel))).To(test.cntMatcher)
			}
			if test.keyMatcher != nil {
				g.Expect(action.watched).Should(test.keyMatcher)
			}
		})
	}
}

func TestDynamicWatchActionInputs(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	mockFn := func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
		return nil
	}

	kind, kindErr := resources.KindForObject(nil, &componentApi.Dashboard{TypeMeta: metav1.TypeMeta{Kind: gvk.Dashboard.Kind}})
	if kindErr != nil {
		t.Fatalf(errFailedToGetKind, kindErr)
	}
	kindLabel := strings.ToLower(kind)

	DynamicWatchResourcesTotal.Reset()
	DynamicWatchResourcesTotal.WithLabelValues(kindLabel).Add(0)

	watches := []watchInput{
		{
			object:  resources.GvkToUnstructured(gvk.Secret),
			dynamic: true,
			// Note: dynamicPredicates field is intentionally not populated in this test
		},
		{
			object:  resources.GvkToUnstructured(gvk.ConfigMap),
			dynamic: true,
			// Note: dynamicPredicates field is intentionally not populated in this test
		},
	}

	action := newDynamicWatch(mockFn, watches)
	err := action.run(ctx, &types.ReconciliationRequest{Instance: &componentApi.Dashboard{
		TypeMeta: metav1.TypeMeta{
			Kind: gvk.Dashboard.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}})

	g.Expect(err).
		ShouldNot(HaveOccurred())
	g.Expect(testutil.ToFloat64(DynamicWatchResourcesTotal.WithLabelValues(kindLabel))).
		Should(BeNumerically("==", 2)) // Both watches register since dynamic predicates are not evaluated
	g.Expect(action.watched).
		Should(And(
			HaveLen(2),
			HaveKey(gvk.Secret),
			HaveKey(gvk.ConfigMap)),
		)
}

func TestDynamicWatchActionNotTwice(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	callCount := 0
	mockFn := func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
		callCount++
		return nil
	}

	kind, kindErr := resources.KindForObject(nil, &componentApi.Dashboard{TypeMeta: metav1.TypeMeta{Kind: gvk.Dashboard.Kind}})
	if kindErr != nil {
		t.Fatalf(errFailedToGetKind, kindErr)
	}
	kindLabel := strings.ToLower(kind)

	DynamicWatchResourcesTotal.Reset()
	DynamicWatchResourcesTotal.WithLabelValues(kindLabel).Add(0)

	watches := []watchInput{
		{
			object:  resources.GvkToUnstructured(gvk.ConfigMap),
			dynamic: true,
			// Note: dynamicPredicates field is intentionally not populated in this test
		},
	}

	action := newDynamicWatch(mockFn, watches)

	err1 := action.run(ctx, &types.ReconciliationRequest{Instance: &componentApi.Dashboard{
		TypeMeta: metav1.TypeMeta{
			Kind: gvk.Dashboard.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}})

	g.Expect(err1).
		ShouldNot(HaveOccurred())

	err2 := action.run(ctx, &types.ReconciliationRequest{Instance: &componentApi.Dashboard{
		TypeMeta: metav1.TypeMeta{
			Kind: gvk.Dashboard.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}})

	g.Expect(err2).
		ShouldNot(HaveOccurred())

	g.Expect(testutil.ToFloat64(DynamicWatchResourcesTotal.WithLabelValues(kindLabel))).
		Should(BeNumerically("==", 1))
	g.Expect(action.watched).
		Should(And(
			HaveLen(1),
			HaveKey(gvk.ConfigMap)),
		)
	g.Expect(callCount).Should(BeNumerically("==", 1))
}
