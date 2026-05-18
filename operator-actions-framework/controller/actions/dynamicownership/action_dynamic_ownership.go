package dynamicownership

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/opendatahub-io/operator-actions-framework/cluster"
	"github.com/opendatahub-io/operator-actions-framework/controller/actions"
	"github.com/opendatahub-io/operator-actions-framework/controller/handlers"
	"github.com/opendatahub-io/operator-actions-framework/controller/predicates"
	resourcespredicates "github.com/opendatahub-io/operator-actions-framework/controller/predicates/resources"
	"github.com/opendatahub-io/operator-actions-framework/controller/types"
	"github.com/opendatahub-io/operator-actions-framework/resources"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var (
	crdGVK        = schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}
	deploymentGVK = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
)

// WatchRegisterFunc is a function type for registering watches dynamically.
type WatchRegisterFunc func(obj client.Object, eventHandler handler.EventHandler, predicates ...predicate.Predicate) error

// ResourceMatcher is a function that determines if a resource matches a certain criteria.
type ResourceMatcher func(res *unstructured.Unstructured) bool

// DefaultManagedByFalseMatcher returns true if the resource has the managed-by annotation set to "false".
// The annotationKey parameter specifies which annotation to check.
func DefaultManagedByFalseMatcher(annotationKey string) ResourceMatcher {
	return func(res *unstructured.Unstructured) bool {
		return resources.GetAnnotation(res, annotationKey) == "false"
	}
}

// Option is a functional option for configuring the Action.
type Option func(*Action)

// WithManagedByFalseMatcher sets a custom matcher for managed-by-false resources.
func WithManagedByFalseMatcher(matcher ResourceMatcher) Option {
	return func(a *Action) {
		if matcher != nil {
			a.managedByFalseMatcher = matcher
		}
	}
}

// WithGVKPredicates sets custom predicates for specific GVKs.
func WithGVKPredicates(gvkPredicates map[schema.GroupVersionKind][]predicate.Predicate) Option {
	return func(a *Action) {
		maps.Copy(a.gvkPredicates, gvkPredicates)
	}
}

// WithPreRegistered pre-populates the watched map with GVKs that already have
// watches registered by the controller builder (via .Owns() or .OwnsGVK()).
func WithPreRegistered(gvks ...schema.GroupVersionKind) Option {
	return func(a *Action) {
		for _, g := range gvks {
			a.watched.Store(watchKey{gvk: g, owned: true}, struct{}{})
		}
	}
}

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

func (a *Action) isWatched(gvk schema.GroupVersionKind, owned bool) bool {
	key := watchKey{gvk: gvk, owned: owned}
	_, ok := a.watched.Load(key)
	return ok
}

func (a *Action) isCRDWatched(crdName string) bool {
	_, ok := a.watchedCRDs.Load(crdName)
	return ok
}

func (a *Action) registerCRDWatchIfNeeded(rr *types.ReconciliationRequest, crdName string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.isCRDWatched(crdName) {
		return false, nil
	}

	if err := a.registerCRDWatch(rr, crdName); err != nil {
		return false, err
	}

	return true, nil
}

func (a *Action) registerWatchIfNeeded(
	rr *types.ReconciliationRequest,
	resGVK schema.GroupVersionKind,
	isOwned bool,
	eventHandler handler.EventHandler,
	watchPredicates []predicate.Predicate,
) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.isWatched(resGVK, isOwned) {
		return false, nil
	}

	obj := resources.GvkToUnstructured(resGVK)
	if err := a.watchRegisterFn(obj, eventHandler, watchPredicates...); err != nil {
		return false, err
	}

	key := watchKey{gvk: resGVK, owned: isOwned}
	a.watched.Store(key, struct{}{})

	if isOwned {
		rr.Controller.AddDynamicOwnedType(resGVK)
	}

	return true, nil
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	if !rr.Controller.IsDynamicOwnershipEnabled() {
		return nil
	}

	l := logf.FromContext(ctx)

	seenKeys := make(map[watchKey]struct{})

	for i := range rr.Resources {
		res := &rr.Resources[i]
		resGVK := res.GroupVersionKind()

		if rr.Controller.IsExcludedFromDynamicOwnership(resGVK) {
			continue
		}

		if resGVK == crdGVK {
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

		isOwned := !a.managedByFalseMatcher(res)

		key := watchKey{gvk: resGVK, owned: isOwned}

		if _, seen := seenKeys[key]; seen {
			continue
		}
		seenKeys[key] = struct{}{}

		if a.isWatched(resGVK, isOwned) {
			continue
		}

		available, err := cluster.IsAPIAvailable(rr.Client, resGVK)
		if err != nil {
			return fmt.Errorf("failed to check API availability for %s: %w", resGVK, err)
		}
		if !available {
			l.V(3).Info("Skipping watch registration, API not available", "gvk", resGVK)
			continue
		}

		var eventHandler handler.EventHandler
		var watchPredicates []predicate.Predicate

		if isOwned {
			eventHandler = handler.EnqueueRequestForOwner(
				rr.Client.Scheme(),
				rr.Client.RESTMapper(),
				resources.GvkToUnstructured(a.ownerGVK),
				handler.OnlyControllerOwner(),
			)
			if customPredicates, ok := a.gvkPredicates[resGVK]; ok {
				watchPredicates = customPredicates
			} else {
				if resGVK == deploymentGVK {
					watchPredicates = []predicate.Predicate{predicates.DefaultDeploymentPredicate}
				} else {
					watchPredicates = []predicate.Predicate{predicates.DefaultPredicate}
				}
			}
		} else {
			eventHandler = handlers.ToNamed(rr.Instance.GetName())
			watchPredicates = []predicate.Predicate{resourcespredicates.Deleted()}
		}

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

func (a *Action) registerCRDWatch(rr *types.ReconciliationRequest, crdName string) error {
	obj := resources.GvkToUnstructured(crdGVK)
	eventHandler := handlers.ToNamed(rr.Instance.GetName())
	namePredicate := resourcespredicates.CreatedOrUpdatedOrDeletedNamed(crdName)
	if err := a.watchRegisterFn(obj, eventHandler, namePredicate); err != nil {
		return fmt.Errorf("failed to register CRD watch for %s: %w", crdName, err)
	}

	a.watchedCRDs.Store(crdName, struct{}{})

	return nil
}

// NewAction creates a new dynamic ownership action.
func NewAction(
	watchRegisterFn WatchRegisterFunc,
	ownerGVK schema.GroupVersionKind,
	opts ...Option,
) actions.Fn {
	action := Action{
		watchRegisterFn:       watchRegisterFn,
		ownerGVK:              ownerGVK,
		managedByFalseMatcher: DefaultManagedByFalseMatcher("opendatahub.io/managed"),
		gvkPredicates:         make(map[schema.GroupVersionKind][]predicate.Predicate),
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}
