package dynamicownership

import (
	"context"
	"fmt"
	"maps"
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

// WatchRegisterFunc is a function type for registering watches dynamically.
type WatchRegisterFunc func(obj client.Object, eventHandler handler.EventHandler, predicates ...predicate.Predicate) error

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
		maps.Copy(a.gvkPredicates, gvkPredicates)
	}
}

// WithPreRegistered pre-populates the watched map with GVKs that already have
// watches registered by the controller builder (via .Owns() or .OwnsGVK()).
// This prevents the dynamic ownership action from registering duplicate watches.
func WithPreRegistered(gvks ...schema.GroupVersionKind) Option {
	return func(a *Action) {
		for _, g := range gvks {
			a.watched.Store(watchKey{gvk: g, owned: true}, struct{}{})
		}
	}
}

// watchKey identifies a unique watch registration.
// We need separate watches for owned resources vs non-owned (managed-by-false) resources.
type watchKey struct {
	gvk   schema.GroupVersionKind
	owned bool
}

// Action registers watches dynamically for deployed resources.
type Action struct {
	watchRegisterFn       WatchRegisterFunc
	ownerGVK              schema.GroupVersionKind
	managedByFalseMatcher ResourceMatcher
	gvkPredicates         map[schema.GroupVersionKind][]predicate.Predicate
	watched               sync.Map
	watchedCRDs           sync.Map
	mu                    sync.Mutex
}

// isWatched returns true if a watch for this GVK and ownership type is already registered.
// It checks only the internal a.watched state, not rr.Controller.Owns(), because the
// deploy action may have already called AddDynamicOwnedType() (making Owns() return true)
// before any watch is actually registered. Static GVKs from the builder are pre-populated
// via WithPreRegistered.
func (a *Action) isWatched(gvk schema.GroupVersionKind, owned bool) bool {
	key := watchKey{gvk: gvk, owned: owned}
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
	isOwned bool,
	eventHandler handler.EventHandler,
	watchPredicates []predicate.Predicate,
) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Re-check under lock to avoid duplicate registration
	if a.isWatched(resGVK, isOwned) {
		return false, nil
	}

	// Register the watch
	obj := resources.GvkToUnstructured(resGVK)
	if err := a.watchRegisterFn(obj, eventHandler, watchPredicates...); err != nil {
		return false, err
	}

	// Track the watch registration
	key := watchKey{gvk: resGVK, owned: isOwned}
	a.watched.Store(key, struct{}{})

	if isOwned {
		// Also register as dynamically owned so Owns() returns true.
		// Idempotent — deploy may have already called this.
		rr.Controller.AddDynamicOwnedType(resGVK)
	}

	return true, nil
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	// Only run if dynamic ownership is enabled
	if !rr.Controller.IsDynamicOwnershipEnabled() {
		return nil
	}

	l := logf.FromContext(ctx)

	// Track what we've seen in this run to avoid duplicate processing
	// We need to track owned and non-owned resources separately
	seenKeys := make(map[watchKey]struct{})

	for i := range rr.Resources {
		res := &rr.Resources[i]
		resGVK := res.GroupVersionKind()

		// Skip if excluded (shared config from builder).
		// Excluded GVKs intentionally have no watch registered by the dynamic ownership action.
		// If the user needs reconciliation triggered by changes to excluded resources,
		// they must set up watches explicitly (e.g., via .Owns, .OwnsGVK(), .Watches() or .WatchesGVK() in the builder).
		if rr.Controller.IsExcludedFromDynamicOwnership(resGVK) {
			continue
		}

		// For CRDs, register a name-based watch (not owner-reference-based) because CRDs
		// are cluster-scoped and do not have owner references set by the deploy action.
		// CRDs can be excluded from ownership like any other GVK via ExcludeGVKs().
		// If excluded, no name-based watch is registered either.
		if resGVK == gvk.CustomResourceDefinition {
			crdName := res.GetName()
			registered, err := a.registerCRDWatchIfNeeded(rr, crdName)
			if err != nil {
				return fmt.Errorf("failed to register CRD watch for %s: %w", crdName, err)
			}
			if registered {
				l.V(3).Info("Registered dynamic CRD watch by name", "crdName", crdName)
			}
			continue
		}

		// Determine if this resource is owned
		isOwned := !a.managedByFalseMatcher(res)

		// Create the watch key for this resource type
		key := watchKey{gvk: resGVK, owned: isOwned}

		// Skip if already processed in this run
		if _, seen := seenKeys[key]; seen {
			continue
		}
		seenKeys[key] = struct{}{}

		// Skip if already watched - quick check without lock
		if a.isWatched(resGVK, isOwned) {
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

		if isOwned {
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
		} else {
			// Watch only for deletion so the controller can recreate the resource if deleted.
			// Note: we cannot filter by managed-by-false annotation here because the deploy
			// action removes the annotation before creating the resource on the cluster.
			// This means when both owned and non-owned resources of the same GVK exist,
			// this watch may fire for owned resource deletions too, causing a redundant
			// (but harmless) reconciliation.
			eventHandler = handlers.ToNamed(rr.Instance.GetName())
			watchPredicates = []predicate.Predicate{resourcespredicates.Deleted()}
		}

		// Register the watch atomically (re-checks under lock to prevent duplicates)
		registered, err := a.registerWatchIfNeeded(rr, resGVK, isOwned, eventHandler, watchPredicates)
		if err != nil {
			return fmt.Errorf("failed to register watch for %s (owned=%t): %w", resGVK, isOwned, err)
		}
		if registered {
			l.V(3).Info("Registered dynamic watch", "gvk", resGVK, "owned", isOwned)
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
	if err := a.watchRegisterFn(obj, eventHandler, namePredicate); err != nil {
		return fmt.Errorf("failed to register CRD watch for %s: %w", crdName, err)
	}

	// Mark CRD as watched by name
	a.watchedCRDs.Store(crdName, struct{}{})

	return nil
}

// NewAction creates a new dynamic ownership action.
//
// This action should run after the deploy action to register watches for
// deployed resources. It iterates rr.Resources and:
//   - For excluded GVKs: skips entirely (no watch, no ownership). The user
//     is responsible for setting up watches for excluded resources.
//   - For CRDs: registers a name-based watch using the CRD's metadata name.
//   - For non-owned (managed-by-false) resources: registers a delete-only watch
//     so the controller can recreate the resource if deleted. These resources
//     are NOT marked as owned (Owns() returns false), protecting them from GC.
//     Note: the managed-by annotation is removed by the deploy action before
//     creating the resource, so annotation-based event filtering is not
//     possible. When both owned and non-owned resources of the same GVK exist,
//     the delete-only watch may fire for owned resource deletions too, causing
//     a redundant (but harmless) reconciliation.
//   - For owned resources: registers a watch with EnqueueRequestForOwner and
//     marks the GVK as dynamically owned via AddDynamicOwnedType().
func NewAction(
	watchRegisterFn WatchRegisterFunc,
	ownerGVK schema.GroupVersionKind,
	opts ...Option,
) actions.Fn {
	action := Action{
		watchRegisterFn:       watchRegisterFn,
		ownerGVK:              ownerGVK,
		managedByFalseMatcher: DefaultManagedByFalseMatcher,
		gvkPredicates:         make(map[schema.GroupVersionKind][]predicate.Predicate),
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}
