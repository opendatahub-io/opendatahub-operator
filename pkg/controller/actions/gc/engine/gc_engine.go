package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/exp/maps"
	authorizationv1 "k8s.io/api/authorization/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	odhcli "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
)

const (
	DeleteVerb  = "delete"
	AnyVerb     = "*"
	AnyResource = "*"
)

type engineOptions struct {
	propagationPolicy ctrlCli.PropagationPolicy
}

type OptsFn func(*engineOptions)

func WithDeletePropagationPolicy(policy metav1.DeletionPropagation) OptsFn {
	return func(o *engineOptions) {
		o.propagationPolicy = ctrlCli.PropagationPolicy(policy)
	}
}

func New(opts ...OptsFn) *GC {
	res := GC{
		options: engineOptions{
			propagationPolicy: ctrlCli.PropagationPolicy(metav1.DeletePropagationForeground),
		},

		resources: Resources{
			items: make([]Resource, 0),
		},
	}

	for _, o := range opts {
		o(&res.options)
	}

	return &res
}

type GC struct {
	resources Resources
	options   engineOptions
}

func (gc *GC) Refresh(
	ctx context.Context,
	cli *odhcli.Client,
	ns string,
) error {
	l := logf.FromContext(ctx)
	l.Info("Start computing deletable types")

	res, err := gc.computeDeletableTypes(ctx, cli, ns)
	if err != nil {
		return fmt.Errorf("cannot discover deletable types: %w", err)
	}

	gc.resources.Set(res)

	l.Info("Deletable types computed", "count", gc.resources.Len())

	return nil
}

type runOptions struct {
	typePredicate   func(context.Context, schema.GroupVersionKind) (bool, error)
	objectPredicate func(context.Context, unstructured.Unstructured) (bool, error)
	selector        labels.Selector
}

type RunOptionsFn func(*runOptions)

func WithObjectFilter(fn func(context.Context, unstructured.Unstructured) (bool, error)) RunOptionsFn {
	return func(o *runOptions) {
		o.objectPredicate = fn
	}
}
func WithTypeFilter(fn func(context.Context, schema.GroupVersionKind) (bool, error)) RunOptionsFn {
	return func(o *runOptions) {
		o.typePredicate = fn
	}
}
func WithSelector(value labels.Selector) RunOptionsFn {
	return func(o *runOptions) {
		o.selector = value
	}
}

func (gc *GC) listResources(
	ctx context.Context,
	cli *odhcli.Client,
	res Resource,
	opts metav1.ListOptions,
) ([]unstructured.Unstructured, error) {
	items, err := cli.Dynamic().Resource(res.GroupVersionResource()).Namespace("").List(ctx, opts)
	switch {
	case k8serr.IsForbidden(err) || k8serr.IsMethodNotSupported(err) || k8serr.IsNotFound(err):
		logf.FromContext(ctx).V(3).Info(
			"cannot list resource",
			"reason", err.Error(),
			"gvk", res.GroupVersionKind(),
		)

		return nil, nil
	case err != nil:
		return nil, err
	default:
		return items.Items, nil
	}
}

func (gc *GC) Run(
	ctx context.Context,
	cli *odhcli.Client,
	opts ...RunOptionsFn,
) (int, error) {
	l := logf.FromContext(ctx)

	ro := runOptions{
		typePredicate: func(_ context.Context, _ schema.GroupVersionKind) (bool, error) {
			return true, nil
		},
		objectPredicate: func(_ context.Context, _ unstructured.Unstructured) (bool, error) {
			return false, nil
		},
	}

	for _, opt := range opts {
		opt(&ro)
	}

	deleted := 0
	resources := gc.resources.Get()
	lo := metav1.ListOptions{}

	if ro.selector != nil {
		lo.LabelSelector = ro.selector.String()
		l.V(3).Info("run", "selector", lo.LabelSelector)
	}

	for _, res := range resources {
		canBeDeleted, err := ro.typePredicate(ctx, res.GroupVersionKind())
		if err != nil {
			return 0, fmt.Errorf("cannot determine if resource %s can be deleted: %w", res.String(), err)
		}

		if !canBeDeleted {
			continue
		}

		items, err := gc.listResources(ctx, cli, res, lo)
		if err != nil {
			return 0, fmt.Errorf("cannot list child resources %s: %w", res.String(), err)
		}

		for i := range items {
			canBeDeleted, err = ro.objectPredicate(ctx, items[i])
			if err != nil {
				return 0, fmt.Errorf("cannot determine if object %s in namespace %q can be deleted: %w",
					items[i].GetName(),
					items[i].GetNamespace(),
					err,
				)
			}
			if !canBeDeleted {
				continue
			}
			if !items[i].GetDeletionTimestamp().IsZero() {
				continue
			}

			if err := gc.delete(ctx, cli, items[i]); err != nil {
				return 0, err
			}

			deleted++
		}
	}

	return deleted, nil
}

func (gc *GC) delete(
	ctx context.Context,
	cli *odhcli.Client,
	resource unstructured.Unstructured,
) error {
	logf.FromContext(ctx).Info(
		"delete",
		"gvk", resource.GroupVersionKind(),
		"ns", resource.GetNamespace(),
		"name", resource.GetName(),
	)

	err := cli.Delete(ctx, &resource, gc.options.propagationPolicy)
	if err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf(
			"cannot delete resources gvk: %s, namespace: %s, name: %s, reason: %w",
			resource.GroupVersionKind().String(),
			resource.GetNamespace(),
			resource.GetName(),
			err,
		)
	}

	return nil
}

func (gc *GC) discoverResources(
	cli *odhcli.Client,
) ([]*metav1.APIResourceList, error) {
	// We rely on the discovery API to retrieve all the resources GVK, that
	// results in an unbounded set that can impact garbage collection latency
	// when scaling up.
	items, err := cli.Discovery().ServerPreferredResources()

	// Swallow group discovery errors, e.g., Knative serving exposes an
	// aggregated API for custom.metrics.k8s.io that requires special
	// authentication scheme while discovering preferred resources.
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		return nil, fmt.Errorf("failure retrieving supported resources: %w", err)
	}

	return items, nil
}

func (gc *GC) computeDeletableTypes(
	ctx context.Context,
	cli *odhcli.Client,
	ns string,
) ([]Resource, error) {
	items, err := gc.discoverResources(cli)
	if err != nil {
		return nil, fmt.Errorf("failure discovering resources: %w", err)
	}

	// We only take types that support the "delete" verb,
	// to prevents from performing queries that we know are going to
	// return "MethodNotAllowed".
	apiResourceLists := discovery.FilteredBy(
		discovery.SupportsAllVerbs{
			Verbs: []string{DeleteVerb},
		},
		items,
	)

	// Get the permissions of the service account in the specified namespace.
	rules, err := gc.retrieveResourceRules(ctx, cli, ns)
	if err != nil {
		return nil, fmt.Errorf("failure retrieving resource rules: %w", err)
	}

	// Collect deletable resources.
	resources, err := gc.collectDeletableResources(apiResourceLists, rules)
	if err != nil {
		return nil, fmt.Errorf("failure retrieving deletable resources: %w", err)
	}

	return resources, nil
}

func (gc *GC) retrieveResourceRules(
	ctx context.Context,
	cli *odhcli.Client,
	ns string,
) ([]authorizationv1.ResourceRule, error) {
	// Retrieve the permissions granted to the operator service account.
	// We assume the operator has only to garbage collect the resources
	// it has created.
	rulesReview := authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{
			Namespace: ns,
		},
	}

	err := cli.Create(ctx, &rulesReview)
	if err != nil {
		return nil, fmt.Errorf("unable to create SelfSubjectRulesReviews: %w", err)
	}

	return rulesReview.Status.ResourceRules, nil
}

func (gc *GC) isResourceDeletable(
	group string,
	apiRes metav1.APIResource,
	rules []authorizationv1.ResourceRule,
) bool {
	for _, rule := range rules {
		if !slices.Contains(rule.Verbs, DeleteVerb) && !slices.Contains(rule.Verbs, AnyVerb) {
			continue
		}
		if !MatchRule(group, apiRes, rule) {
			continue
		}

		return true
	}

	return false
}

func (gc *GC) collectDeletableResources(
	apiResourceLists []*metav1.APIResourceList,
	rules []authorizationv1.ResourceRule,
) ([]Resource, error) {
	resp := make(map[Resource]struct{})

	for i := range apiResourceLists {
		res := apiResourceLists[i]

		for _, apiRes := range res.APIResources {
			resourceGroup := apiRes.Group
			if resourceGroup == "" {
				gv, err := schema.ParseGroupVersion(res.GroupVersion)
				if err != nil {
					return nil, fmt.Errorf("unable to parse group version: %w", err)
				}

				resourceGroup = gv.Group
			}

			if !gc.isResourceDeletable(resourceGroup, apiRes, rules) {
				continue
			}

			gv, err := schema.ParseGroupVersion(res.GroupVersion)
			if err != nil {
				return nil, err
			}

			gvr := Resource{
				RESTMapping: meta.RESTMapping{
					Resource: schema.GroupVersionResource{
						Group:    gv.Group,
						Version:  gv.Version,
						Resource: apiRes.Name,
					},
					GroupVersionKind: schema.GroupVersionKind{
						Group:   gv.Group,
						Version: gv.Version,
						Kind:    apiRes.Kind,
					},
					Scope: meta.RESTScopeNamespace,
				},
			}

			if !apiRes.Namespaced {
				gvr.Scope = meta.RESTScopeRoot
			}

			resp[gvr] = struct{}{}
		}
	}

	resources := maps.Keys(resp)
	slices.SortFunc(resources, func(a, b Resource) int {
		return strings.Compare(a.String(), b.String())
	})

	return resources, nil
}
