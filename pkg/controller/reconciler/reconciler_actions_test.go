//nolint:testpackage
package reconciler

import (
	"context"
	"testing"

	gomegaTypes "github.com/onsi/gomega/types"
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

func TestDynamicWatchAction_Run(t *testing.T) {
	tests := []struct {
		name       string
		object     common.PlatformObject
		preds      []DynamicPredicate
		errMatcher gomegaTypes.GomegaMatcher
		cntMatcher gomegaTypes.GomegaMatcher
		keyMatcher gomegaTypes.GomegaMatcher
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
				func(_ context.Context, rr *types.ReconciliationRequest) bool {
					return true
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
				func(_ context.Context, rr *types.ReconciliationRequest) bool {
					return rr.Instance.GetGeneration() > 0
				},
				func(_ context.Context, rr *types.ReconciliationRequest) bool {
					return rr.Instance.GetResourceVersion() != ""
				},
			},
			errMatcher: Not(HaveOccurred()),
			cntMatcher: BeNumerically("==", 1),
			keyMatcher: HaveKey(gvk.ConfigMap),
		},

		{
			name:   "should not register a watcher the predicate returns false",
			object: &componentApi.Dashboard{TypeMeta: metav1.TypeMeta{Kind: gvk.Dashboard.Kind}},
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
			ctx := context.Background()

			watches := []watchInput{{
				object:      resources.GvkToUnstructured(gvk.ConfigMap),
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
	ctx := context.Background()

	mockFn := func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
		return nil
	}

	DynamicWatchResourcesTotal.Reset()
	DynamicWatchResourcesTotal.WithLabelValues("dashboard").Add(0)

	watches := []watchInput{
		{
			object:  resources.GvkToUnstructured(gvk.Secret),
			dynamic: true,
			dynamicPred: []DynamicPredicate{func(_ context.Context, rr *types.ReconciliationRequest) bool {
				return rr.Instance.GetGeneration() == 0
			}},
		},
		{
			object:  resources.GvkToUnstructured(gvk.ConfigMap),
			dynamic: true,
			dynamicPred: []DynamicPredicate{func(_ context.Context, rr *types.ReconciliationRequest) bool {
				return rr.Instance.GetGeneration() > 0
			}},
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
	g.Expect(testutil.ToFloat64(DynamicWatchResourcesTotal)).
		Should(BeNumerically("==", 1))
	g.Expect(action.watched).
		Should(And(
			HaveLen(1),
			HaveKey(gvk.ConfigMap)),
		)
}

func TestDynamicWatchAction_NotTwice(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	mockFn := func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
		return nil
	}

	DynamicWatchResourcesTotal.Reset()
	DynamicWatchResourcesTotal.WithLabelValues("dashboard").Add(0)

	watches := []watchInput{
		{
			object:  resources.GvkToUnstructured(gvk.ConfigMap),
			dynamic: true,
			dynamicPred: []DynamicPredicate{func(_ context.Context, rr *types.ReconciliationRequest) bool {
				return rr.Instance.GetGeneration() > 0
			}},
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

	g.Expect(testutil.ToFloat64(DynamicWatchResourcesTotal)).
		Should(BeNumerically("==", 1))
	g.Expect(action.watched).
		Should(And(
			HaveLen(1),
			HaveKey(gvk.ConfigMap)),
		)
}
