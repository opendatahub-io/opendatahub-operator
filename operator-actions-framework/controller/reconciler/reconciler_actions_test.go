//nolint:testpackage
package reconciler

import (
	"context"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	"github.com/opendatahub-io/operator-actions-framework/api"
	"github.com/opendatahub-io/operator-actions-framework/controller/types"
	"github.com/opendatahub-io/operator-actions-framework/resources"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	. "github.com/onsi/gomega"
)

var (
	testGVKConfigMap = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	testGVKSecret    = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	testGVKDashboard = schema.GroupVersionKind{Group: "components.opendatahub.io", Version: "v1alpha1", Kind: "Dashboard"}
)

type testPlatformObject struct {
	unstructured.Unstructured

	status api.Status
}

func (t *testPlatformObject) GetStatus() *api.Status {
	return &t.status
}

func (t *testPlatformObject) GetConditions() []api.Condition {
	return nil
}

func (t *testPlatformObject) SetConditions(_ []api.Condition) {}

func newTestPlatformObject(gvk schema.GroupVersionKind, opts ...func(*unstructured.Unstructured)) *testPlatformObject {
	obj := &testPlatformObject{}
	obj.SetGroupVersionKind(gvk)
	for _, o := range opts {
		o(&obj.Unstructured)
	}
	return obj
}

func TestDynamicWatchAction_Run(t *testing.T) {
	tests := []struct {
		name       string
		object     *testPlatformObject
		preds      []DynamicPredicate
		errMatcher gTypes.GomegaMatcher
		cntMatcher gTypes.GomegaMatcher
		keyMatcher gTypes.GomegaMatcher
	}{
		{
			name:       "should register a watcher if no predicates",
			object:     newTestPlatformObject(testGVKDashboard),
			preds:      []DynamicPredicate{},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 1),
			keyMatcher: HaveKey(testGVKConfigMap),
		},

		{
			name:   "should register a watcher when the predicate evaluate to true",
			object: newTestPlatformObject(testGVKDashboard),
			preds: []DynamicPredicate{
				func(_ context.Context, rr *types.ReconciliationRequest) bool {
					return true
				},
			},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 1),
			keyMatcher: HaveKey(testGVKConfigMap),
		},

		{
			name: "should register a watcher when all predicates evaluate to true",
			object: newTestPlatformObject(testGVKDashboard, func(u *unstructured.Unstructured) {
				u.SetGeneration(1)
				u.SetResourceVersion(xid.New().String())
			}),
			preds: []DynamicPredicate{
				func(_ context.Context, rr *types.ReconciliationRequest) bool {
					return rr.Instance.GetGeneration() > 0
				},
				func(_ context.Context, rr *types.ReconciliationRequest) bool {
					return rr.Instance.GetResourceVersion() != ""
				},
			},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 1),
			keyMatcher: HaveKey(testGVKConfigMap),
		},

		{
			name:   "should not register a watcher the predicate returns false",
			object: newTestPlatformObject(testGVKDashboard),
			preds: []DynamicPredicate{
				func(_ context.Context, rr *types.ReconciliationRequest) bool {
					return false
				},
			},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 0),
			keyMatcher: BeEmpty(),
		},

		{
			name: "should not register a watcher when a predicate returns false",
			object: newTestPlatformObject(testGVKDashboard, func(u *unstructured.Unstructured) {
				u.SetGeneration(1)
			}),
			preds: []DynamicPredicate{
				func(_ context.Context, rr *types.ReconciliationRequest) bool {
					return rr.Instance.GetGeneration() > 0
				},
				func(_ context.Context, rr *types.ReconciliationRequest) bool {
					return rr.Instance.GetResourceVersion() != ""
				},
			},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 0),
			keyMatcher: BeEmpty(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			watches := []watchInput{{
				object:      resources.GvkToUnstructured(testGVKConfigMap),
				dynamic:     true,
				dynamicPred: test.preds,
			}}

			mockFn := func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
				return nil
			}

			DynamicWatchResourcesTotal.Reset()
			DynamicWatchResourcesTotal.WithLabelValues("dashboard").Add(0)

			action := newDynamicWatch(mockFn, watches)
			err := action.run(ctx, &types.ReconciliationRequest{Instance: test.object})

			if test.errMatcher != nil {
				g.Expect(err).To(test.errMatcher)
			}
			if test.cntMatcher != nil {
				g.Expect(testutil.ToFloat64(DynamicWatchResourcesTotal)).To(test.cntMatcher)
			}
			if test.keyMatcher != nil {
				g.Expect(action.watched).Should(test.keyMatcher)
			}
		})
	}
}

func TestDynamicWatchAction_Inputs(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	mockFn := func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
		return nil
	}

	DynamicWatchResourcesTotal.Reset()
	DynamicWatchResourcesTotal.WithLabelValues("dashboard").Add(0)

	watches := []watchInput{
		{
			object:  resources.GvkToUnstructured(testGVKSecret),
			dynamic: true,
			dynamicPred: []DynamicPredicate{func(_ context.Context, rr *types.ReconciliationRequest) bool {
				return rr.Instance.GetGeneration() == 0
			}},
		},
		{
			object:  resources.GvkToUnstructured(testGVKConfigMap),
			dynamic: true,
			dynamicPred: []DynamicPredicate{func(_ context.Context, rr *types.ReconciliationRequest) bool {
				return rr.Instance.GetGeneration() > 0
			}},
		},
	}

	instance := newTestPlatformObject(testGVKDashboard, func(u *unstructured.Unstructured) {
		u.SetGeneration(1)
	})

	action := newDynamicWatch(mockFn, watches)
	err := action.run(ctx, &types.ReconciliationRequest{Instance: instance})

	g.Expect(err).
		ShouldNot(HaveOccurred())
	g.Expect(testutil.ToFloat64(DynamicWatchResourcesTotal)).
		Should(BeNumerically("==", 1))
	g.Expect(action.watched).
		Should(And(
			HaveLen(1),
			HaveKey(testGVKConfigMap)),
		)
}

func TestDynamicWatchAction_NotTwice(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	mockFn := func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
		return nil
	}

	DynamicWatchResourcesTotal.Reset()
	DynamicWatchResourcesTotal.WithLabelValues("dashboard").Add(0)

	watches := []watchInput{
		{
			object:  resources.GvkToUnstructured(testGVKConfigMap),
			dynamic: true,
			dynamicPred: []DynamicPredicate{func(_ context.Context, rr *types.ReconciliationRequest) bool {
				return rr.Instance.GetGeneration() > 0
			}},
		},
	}

	instance := newTestPlatformObject(testGVKDashboard, func(u *unstructured.Unstructured) {
		u.SetGeneration(1)
	})

	action := newDynamicWatch(mockFn, watches)

	err1 := action.run(ctx, &types.ReconciliationRequest{Instance: instance})

	g.Expect(err1).
		ShouldNot(HaveOccurred())

	err2 := action.run(ctx, &types.ReconciliationRequest{Instance: instance})

	g.Expect(err2).
		ShouldNot(HaveOccurred())

	g.Expect(testutil.ToFloat64(DynamicWatchResourcesTotal)).
		Should(BeNumerically("==", 1))
	g.Expect(action.watched).
		Should(And(
			HaveLen(1),
			HaveKey(testGVKConfigMap)),
		)
}
