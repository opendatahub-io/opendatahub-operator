package reconciler_test

import (
	"fmt"
	"reflect"
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
	require.NotNil(t, result, "WithBroadPredicate should not return nil")

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
	require.NotNil(t, result, "WithBroadPredicate should not return nil")

	// Should return the same builder instance (fluent API) - pointer identity check
	assert.Same(t, rb, result)

	// Verify no new instance was created - compare addresses
	assert.Equal(t, fmt.Sprintf("%p", rb), fmt.Sprintf("%p", result), "WithBroadPredicate should return the same instance, not create a new one")

	// Assert the builder's internal state equals the expected "broad" configuration
	// The useBroadPredicate field should be true after calling WithBroadPredicate()
	// We can access this through reflection since it's an unexported field
	rbValue := reflect.ValueOf(rb).Elem()
	useBroadPredicateField := rbValue.FieldByName("useBroadPredicate")
	require.True(t, useBroadPredicateField.IsValid(), "useBroadPredicate field should exist")
	assert.True(t, useBroadPredicateField.Bool(), "useBroadPredicate should be true after calling WithBroadPredicate()")
}
