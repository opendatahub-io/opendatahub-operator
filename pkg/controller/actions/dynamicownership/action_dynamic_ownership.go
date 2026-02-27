package dynamicownership

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	resourcespredicates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// WatchRegistrar is a function type for registering watches dynamically.
type WatchRegistrar func(obj client.Object, eventHandler handler.EventHandler, predicates ...predicate.Predicate) error

// ResourceMatcher is a function that determines if a resource matches a certain criteria.
type ResourceMatcher func(res *unstructured.Unstructured) bool

// DefaultManagedByFalseMatcher returns true if the resource has the managed-by-operator annotation set to "false".
func DefaultManagedByFalseMatcher(res *unstructured.Unstructured) bool {
	return resources.GetAnnotation(res, annotations.ManagedByODHOperator) == "false"
}

// Option is a functional option for configuring the Action.
type Option func(*Action)

// WithManagedByFalseMatcher sets a custom matcher for managed-by-false resources.
// If matcher is nil, the option is ignored and the default matcher is retained.
func WithManagedByFalseMatcher(matcher ResourceMatcher) Option {
	return func(a *Action) {
		if matcher != nil {
			a.managedByFalseMatcher = matcher
		}
	}
}

// WithGVKPredicates sets custom predicates for specific GVKs.
// These predicates will be used instead of the default predicates for the specified GVKs.
func WithGVKPredicates(gvkPredicates map[schema.GroupVersionKind][]predicate.Predicate) Option {
	return func(a *Action) {
		a.gvkPredicates = gvkPredicates
	}
}

// watchKey identifies a unique watch registration.
// We need separate watches for normal resources vs managed-by-false resources.
type watchKey struct {
	gvk            schema.GroupVersionKind
	managedByFalse bool
}

// Action registers watches dynamically for deployed resources.
type Action struct {
	watchRegistrar        WatchRegistrar
	ownerGVK              schema.GroupVersionKind
	managedByFalseMatcher ResourceMatcher
	gvkPredicates         map[schema.GroupVersionKind][]predicate.Predicate
	watched               sync.Map
	watchedCRDs           sync.Map
	mu                    sync.Mutex
}

// isWatched returns true if the GVK with the given managed-by-false status is already being watched.
func (a *Action) isWatched(rr *types.ReconciliationRequest, gvk schema.GroupVersionKind, managedByFalse bool) bool {
	// For normal resources (not managed-by-false), check static ownership from builder
	if !managedByFalse && rr.Controller.Owns(gvk) {
		return true
	}
	// Check dynamic watches registered by this action
	key := watchKey{gvk: gvk, managedByFalse: managedByFalse}
	_, ok := a.watched.Load(key)
	return ok
}

// isCRDWatched returns true if a CRD with the given name is already being watched.
func (a *Action) isCRDWatched(crdName string) bool {
	_, ok := a.watchedCRDs.Load(crdName)
	return ok
}

// registerCRDWatchIfNeeded atomically checks if a CRD watch is needed and registers it.
// Returns true if the watch was registered, false if it was already registered.
func (a *Action) registerCRDWatchIfNeeded(rr *types.ReconciliationRequest, crdName string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Re-check under lock to avoid duplicate registration
	if a.isCRDWatched(crdName) {
		return false, nil
	}

	if err := a.registerCRDWatch(rr, crdName); err != nil {
		return false, err
	}

	return true, nil
}

// registerWatchIfNeeded atomically checks if a watch is needed and registers it.
// Returns true if the watch was registered, false if it was already registered or skipped.
func (a *Action) registerWatchIfNeeded(
	rr *types.ReconciliationRequest,
	resGVK schema.GroupVersionKind,
	isManagedByFalse bool,
	eventHandler handler.EventHandler,
	watchPredicates []predicate.Predicate,
) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Re-check under lock to avoid duplicate registration
	if a.isWatched(rr, resGVK, isManagedByFalse) {
		return false, nil
	}

	// Register the watch
	obj := resources.GvkToUnstructured(resGVK)
	if err := a.watchRegistrar(obj, eventHandler, watchPredicates...); err != nil {
		return false, err
	}

	// Mark as watched internally
	key := watchKey{gvk: resGVK, managedByFalse: isManagedByFalse}
	a.watched.Store(key, struct{}{})

	return true, nil
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	// Only run if dynamic ownership is enabled
	if !rr.Controller.IsDynamicOwnershipEnabled() {
		return nil
	}

	l := logf.FromContext(ctx)

	// Track what we've seen in this run to avoid duplicate processing
	// We need to track normal and managed-by-false resources separately
	seenKeys := make(map[watchKey]struct{})

	for i := range rr.Resources {
		res := &rr.Resources[i]
		resGVK := res.GroupVersionKind()

		// Skip if excluded (shared config from builder)
		if rr.Controller.IsExcludedFromOwnership(resGVK) {
			continue
		}

		// For CRDs, check if we should watch by name
		if resGVK == gvk.CustomResourceDefinition {
			crdName := res.GetName()
			registered, err := a.registerCRDWatchIfNeeded(rr, crdName)
			if err != nil {
				l.Error(err, "Failed to register CRD watch", "name", crdName)
				// Continue - don't fail the entire reconciliation for watch errors
			} else if registered {
				l.V(3).Info("Registered dynamic CRD watch by name", "crdName", crdName)
			}
			continue
		}

		// Determine if this is a managed-by-false resource
		isManagedByFalse := a.managedByFalseMatcher(res)

		// Create the watch key for this resource type
		key := watchKey{gvk: resGVK, managedByFalse: isManagedByFalse}

		// Skip if already processed in this run
		if _, seen := seenKeys[key]; seen {
			continue
		}
		seenKeys[key] = struct{}{}

		// Skip if already watched (static or dynamic) - quick check without lock
		if a.isWatched(rr, resGVK, isManagedByFalse) {
			continue
		}

		available, err := cluster.IsAPIAvailable(rr.Client, resGVK)
		if err != nil {
			return fmt.Errorf("failed to check API availability for %s: %w", resGVK, err)
		}
		if !available {
			// API not available yet, skip - will retry next reconciliation
			l.V(3).Info("Skipping watch registration, API not available", "gvk", resGVK)
			continue
		}

		var eventHandler handler.EventHandler
		var watchPredicates []predicate.Predicate

		if isManagedByFalse {
			// For managed-by: false resources, use a handler that only triggers on delete
			eventHandler = handlers.ToNamed(rr.Instance.GetName())
			watchPredicates = []predicate.Predicate{resourcespredicates.Deleted()}
		} else {
			// For normal owned resources, use EnqueueRequestForOwner
			eventHandler = handler.EnqueueRequestForOwner(
				rr.Client.Scheme(),
				rr.Client.RESTMapper(),
				resources.GvkToUnstructured(a.ownerGVK),
				handler.OnlyControllerOwner(),
			)
			// Check for custom predicates for this GVK
			if customPredicates, ok := a.gvkPredicates[resGVK]; ok {
				watchPredicates = customPredicates
			} else {
				watchPredicates = []predicate.Predicate{predicates.DefaultPredicate}
			}
		}

		// Register the watch atomically (re-checks under lock to prevent duplicates)
		registered, err := a.registerWatchIfNeeded(rr, resGVK, isManagedByFalse, eventHandler, watchPredicates)
		if err != nil {
			l.Error(err, "Failed to register watch for dynamic ownership", "gvk", resGVK, "managedByFalse", isManagedByFalse)
			// Continue - don't fail the entire reconciliation for watch errors
			continue
		}
		if registered {
			l.V(3).Info("Registered dynamic watch", "gvk", resGVK, "managedByFalse", isManagedByFalse)
		}
	}

	return nil
}

// registerCRDWatch registers a watch for a specific CRD by name and marks it as watched.
// This method must be called while holding the mutex.
func (a *Action) registerCRDWatch(rr *types.ReconciliationRequest, crdName string) error {
	// Register the watch for CRDs filtered by name
	obj := resources.GvkToUnstructured(gvk.CustomResourceDefinition)
	eventHandler := handlers.ToNamed(rr.Instance.GetName())
	namePredicate := resourcespredicates.CreatedOrUpdatedOrDeletedNamed(crdName)
	if err := a.watchRegistrar(obj, eventHandler, namePredicate); err != nil {
		return fmt.Errorf("failed to register CRD watch for %s: %w", crdName, err)
	}

	// Mark CRD as watched by name
	a.watchedCRDs.Store(crdName, struct{}{})

	return nil
}

// NewAction creates a new dynamic ownership action.
// This action should run after the deploy action to register watches for deployed resources.
func NewAction(
	watchRegistrar WatchRegistrar,
	ownerGVK schema.GroupVersionKind,
	opts ...Option,
) actions.Fn {
	action := Action{
		watchRegistrar:        watchRegistrar,
		ownerGVK:              ownerGVK,
		managedByFalseMatcher: DefaultManagedByFalseMatcher,
		gvkPredicates:         make(map[schema.GroupVersionKind][]predicate.Predicate),
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}
