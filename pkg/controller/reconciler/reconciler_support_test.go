package reconciler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

func TestReconcilerBuilder_WithBroadPredicate(t *testing.T) {
	// Create a mock manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{})
	require.NoError(t, err)

	// Create a test object
	obj := &v1alpha1.Dashboard{}

	// Test that WithBroadPredicate returns the builder (fluent API)
	builder := reconciler.ReconcilerFor(mgr, obj)
	result := builder.WithBroadPredicate()

	// Should return the same builder instance for fluent API
	assert.Equal(t, builder, result)
}

func TestReconcilerBuilder_CustomOptionsOverride(t *testing.T) {
	// Create a mock manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{})
	require.NoError(t, err)

	// Create a test object
	obj := &v1alpha1.Dashboard{}

	// Test that custom options work with WithBroadPredicate
	customPredicate := predicate.GenerationChangedPredicate{}
	builder := reconciler.ReconcilerFor(mgr, obj, builder.WithPredicates(customPredicate))

	// Enable broad predicate
	result := builder.WithBroadPredicate()

	// Should return the same builder instance
	assert.Equal(t, builder, result)
}
