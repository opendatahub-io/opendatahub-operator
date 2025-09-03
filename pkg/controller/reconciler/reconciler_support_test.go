package reconciler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	errWithBroadPredicateNil = "WithBroadPredicate should not return nil"
)

func newHermeticManager(t *testing.T) ctrl.Manager {
	t.Helper()
	// Create a hermetic manager (no real cluster) with registered scheme
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	cfg := &rest.Config{Host: "https://127.0.0.1:1"} // not contacted; manager isn't started
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
	})
	require.NoError(t, err)
	return mgr
}

func TestReconcilerBuilderWithBroadPredicate(t *testing.T) {
	t.Parallel()
	mgr := newHermeticManager(t)

	// Create a test object
	obj := &v1alpha1.Dashboard{}

	// Test that WithBroadPredicate returns the builder (fluent API)
	rb := reconciler.ReconcilerFor(mgr, obj)
	result := rb.WithBroadPredicate()

	// Fail fast if result is nil before comparing pointers
	require.NotNil(t, result, errWithBroadPredicateNil)

	// Should return the same builder instance (fluent API)
	assert.Same(t, rb, result)
}

func TestReconcilerBuilderCustomOptionsFluent(t *testing.T) {
	t.Parallel()
	mgr := newHermeticManager(t)

	// Create a test object
	obj := &v1alpha1.Dashboard{}

	// Test that custom options work with WithBroadPredicate
	rb := reconciler.ReconcilerFor(mgr, obj, builder.WithPredicates(predicate.GenerationChangedPredicate{}))

	// Enable broad predicate
	result := rb.WithBroadPredicate()

	// Fail fast if result is nil before comparing pointers
	require.NotNil(t, result, errWithBroadPredicateNil)

	// Should return the same builder instance (fluent API) - pointer identity check
	assert.Same(t, rb, result)
}

func TestReconcilerBuilderBroadPredicateBehavior(t *testing.T) {
	t.Parallel()
	mgr := newHermeticManager(t)

	// Create a test object
	obj := &v1alpha1.Dashboard{}

	// Test that the fluent API works correctly - calling WithBroadPredicate multiple times
	// should not change the behavior and should return the same instance
	rb := reconciler.ReconcilerFor(mgr, obj)

	// First call to WithBroadPredicate
	result1 := rb.WithBroadPredicate()
	require.NotNil(t, result1, errWithBroadPredicateNil)
	assert.Same(t, rb, result1, "WithBroadPredicate should return the same instance for fluent API")

	// Second call to WithBroadPredicate should return the same instance
	result2 := result1.WithBroadPredicate()
	require.NotNil(t, result2, errWithBroadPredicateNil)
	assert.Same(t, rb, result2, "WithBroadPredicate should return the same instance for fluent API")
	assert.Same(t, result1, result2, "Multiple calls to WithBroadPredicate should return the same instance")

	// Test that the fluent API works with other methods
	result3 := result2.WithAction(func(ctx context.Context, req *types.ReconciliationRequest) error {
		return nil
	})
	require.NotNil(t, result3, "WithAction should not return nil")
	assert.Same(t, rb, result3, "WithAction should return the same instance for fluent API")
}
