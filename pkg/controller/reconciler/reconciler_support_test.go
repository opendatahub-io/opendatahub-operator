package reconciler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

func TestReconcilerBuilderWithBroadPredicate(t *testing.T) {
	t.Parallel()
	// Create a hermetic manager (no real cluster) with registered scheme
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	cfg := &rest.Config{Host: "https://127.0.0.1:1"} // not contacted; manager isn't started
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme})
	require.NoError(t, err)

	// Create a test object
	obj := &v1alpha1.Dashboard{}

	// Test that WithBroadPredicate returns the builder (fluent API)
	rb := reconciler.ReconcilerFor(mgr, obj)
	result := rb.WithBroadPredicate()

	// Should return the same builder instance (fluent API)
	assert.Same(t, rb, result)
}

func TestReconcilerBuilderCustomOptionsFluent(t *testing.T) {
	t.Parallel()
	// Create a hermetic manager (no real cluster) with registered scheme
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	cfg := &rest.Config{Host: "https://127.0.0.1:1"} // not contacted; manager isn't started
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme})
	require.NoError(t, err)

	// Create a test object
	obj := &v1alpha1.Dashboard{}

	// Test that custom options work with WithBroadPredicate
	customPredicate := predicate.GenerationChangedPredicate{}
	rb := reconciler.ReconcilerFor(mgr, obj, builder.WithPredicates(customPredicate))

	// Enable broad predicate
	result := rb.WithBroadPredicate()

	// Should return the same builder instance (fluent API)
	assert.Same(t, rb, result)
}
