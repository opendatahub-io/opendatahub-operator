package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/opendatahub-io/operator-actions-framework/cluster/gvk"
	"github.com/opendatahub-io/operator-actions-framework/controller/actions"
	odhTypes "github.com/opendatahub-io/operator-actions-framework/controller/types"
	"github.com/opendatahub-io/operator-actions-framework/metadata"
	"github.com/opendatahub-io/operator-actions-framework/resources"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type Mode string

const (
	ModePatch Mode = "patch"
	ModeSSA   Mode = "ssa"

	DefaultPartOfLabelKey      = "platform.opendatahub.io/part-of"
	DefaultAnnotationPrefix    = "platform.opendatahub.io"
	DefaultManagedByAnnotation = "opendatahub.io/managed"
	DefaultPartOfLabelValue    = "platform"
)

// CustomizerFn is a function that customizes a resource during deployment.
// It receives the deploy client, the desired object, and the current cluster state (nil if new).
// It can modify obj in place. Return nil to continue with default deployment logic.
type CustomizerFn func(ctx context.Context, cli client.Client, action *Action, obj *unstructured.Unstructured, old *unstructured.Unstructured) error

// SortFn defines a function that reorders resources before deployment.
type SortFn func(ctx context.Context, resources []unstructured.Unstructured) ([]unstructured.Unstructured, error)

func (s SortFn) Then(then SortFn) SortFn {
	return func(ctx context.Context, resources []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
		output, err := s(ctx, resources)
		if err != nil {
			return nil, err
		}
		return then(ctx, output)
	}
}

// Action deploys the resources that are included in the ReconciliationRequest using
// the same create or patch machinery implemented as part of deploy.DeployManifestsFromPath.
type Action struct {
	fieldOwner            string
	deployMode            Mode
	partOfLabelKey        string
	defaultPartOfLabelKey string
	partOfLabelDefault    string
	annotationPrefix      string
	managedByAnnotation   string
	labels                map[string]string
	annotations           map[string]string
	cache                 *Cache
	sortFn                SortFn
	continueOnError       bool
	applyCustomizers      map[schema.GroupVersionKind]CustomizerFn
	patchCustomizers      map[schema.GroupVersionKind]CustomizerFn
}

type ActionOpts func(*Action)

func WithFieldOwner(value string) ActionOpts {
	return func(action *Action) {
		action.fieldOwner = value
	}
}

func WithMode(value Mode) ActionOpts {
	return func(action *Action) {
		action.deployMode = value
	}
}

func WithPartOfLabel(key string) ActionOpts {
	return func(action *Action) {
		action.partOfLabelKey = key
	}
}

// WithPartOfLabelDefault sets the default value for the part-of label when none is set.
func WithPartOfLabelDefault(value string) ActionOpts {
	return func(action *Action) {
		action.partOfLabelDefault = value
	}
}

func WithAnnotationPrefix(prefix string) ActionOpts {
	return func(action *Action) {
		action.annotationPrefix = prefix
	}
}

// WithManagedByAnnotation sets the annotation key used to mark resources as managed/unmanaged.
func WithManagedByAnnotation(key string) ActionOpts {
	return func(action *Action) {
		action.managedByAnnotation = key
	}
}

func WithLabel(name string, value string) ActionOpts {
	return func(action *Action) {
		if action.labels == nil {
			action.labels = map[string]string{}
		}

		action.labels[name] = value
	}
}

func WithLabels(values map[string]string) ActionOpts {
	return func(action *Action) {
		if action.labels == nil {
			action.labels = map[string]string{}
		}

		maps.Copy(action.labels, values)
	}
}

func WithAnnotation(name string, value string) ActionOpts {
	return func(action *Action) {
		if action.annotations == nil {
			action.annotations = map[string]string{}
		}

		action.annotations[name] = value
	}
}

func WithAnnotations(values map[string]string) ActionOpts {
	return func(action *Action) {
		if action.annotations == nil {
			action.annotations = map[string]string{}
		}

		maps.Copy(action.annotations, values)
	}
}

func WithCache(opts ...CacheOpt) ActionOpts {
	return func(action *Action) {
		action.cache = NewCache(opts...)
	}
}

// WithSortFn sets a custom sort function to reorder resources before deploying.
func WithSortFn(fn SortFn) ActionOpts {
	return func(action *Action) {
		action.sortFn = fn
	}
}

// WithApplyOrder is a convenience option that sorts resources into
// dependency order (CRDs first, webhooks last) before deploying.
func WithApplyOrder() ActionOpts {
	return WithSortFn(resources.SortByApplyOrder)
}

// WithContinueOnError makes the deploy action continue applying remaining
// resources when an individual resource fails instead of stopping at the
// first error. All errors are collected and returned as a joined error.
func WithContinueOnError() ActionOpts {
	return func(action *Action) {
		action.continueOnError = true
	}
}

// WithApplyCustomizer registers a customizer for a specific GVK that runs during SSA apply mode.
// The customizer can modify the object before it's applied.
func WithApplyCustomizer(gvk schema.GroupVersionKind, fn CustomizerFn) ActionOpts {
	return func(action *Action) {
		action.applyCustomizers[gvk] = fn
	}
}

// WithPatchCustomizer registers a customizer for a specific GVK that runs during patch mode.
// The customizer can modify the object before it's patched.
func WithPatchCustomizer(gvk schema.GroupVersionKind, fn CustomizerFn) ActionOpts {
	return func(action *Action) {
		action.patchCustomizers[gvk] = fn
	}
}

// resolveFieldOwner returns the effective field owner for the reconciled instance.
func (a *Action) resolveFieldOwner(rr *odhTypes.ReconciliationRequest) (string, error) {
	if a.fieldOwner != "" {
		return a.fieldOwner, nil
	}

	kind, err := resources.KindForObject(rr.Client.Scheme(), rr.Instance)
	if err != nil {
		return "", err
	}

	return strings.ToLower(kind), nil
}

func (a *Action) run(ctx context.Context, rr *odhTypes.ReconciliationRequest) error {
	if rr.SkipDeploy {
		return nil
	}

	if a.sortFn != nil {
		sorted, err := a.sortFn(ctx, rr.Resources)
		if err != nil {
			return fmt.Errorf("failed to sort resources: %w", err)
		}
		rr.Resources = sorted
	}

	if a.cache != nil {
		a.cache.Sync()
	}

	controllerName, err := a.resolveFieldOwner(rr)
	if err != nil {
		return err
	}

	igvk := rr.Instance.GetObjectKind().GroupVersionKind()

	crdGVK := schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	}

	var firstErr error
	var failedResources []string

	for i := range rr.Resources {
		res := rr.Resources[i]
		current := resources.GvkToUnstructured(res.GroupVersionKind())

		lookupErr := rr.Client.Get(ctx, client.ObjectKeyFromObject(&res), current)
		switch {
		case k8serr.IsNotFound(lookupErr):
			current = nil
		case lookupErr != nil:
			if !a.continueOnError {
				return fmt.Errorf("failed to lookup object %s/%s: %w", res.GetNamespace(), res.GetName(), lookupErr)
			}

			resErr := fmt.Errorf("failed to lookup object %s/%s: %w", res.GetNamespace(), res.GetName(), lookupErr)
			logf.FromContext(ctx).Error(lookupErr, "resource lookup failed, continuing",
				"namespace", res.GetNamespace(), "name", res.GetName())

			if firstErr == nil {
				firstErr = resErr
			}

			failedResources = append(failedResources, res.GetNamespace()+"/"+res.GetName())

			continue
		default:
			if err := resources.RemoveOwnerReferences(ctx, rr.Client, current, ownedTypeIsNot(&igvk)); err != nil {
				return err
			}

			if resources.GetAnnotation(current, a.managedByAnnotation) == "false" {
				if err := resources.RemoveOwnerReferences(ctx, rr.Client, current, ownedTypeIs(&igvk)); err != nil {
					return err
				}

				continue
			}
		}

		var ok bool
		var err error

		switch rr.Resources[i].GroupVersionKind() {
		case crdGVK:
			ok, err = a.deployCRD(ctx, rr, res, current)
		default:
			ok, err = a.deploy(ctx, rr, res, current)
		}

		if err != nil {
			if !a.continueOnError {
				return fmt.Errorf("failure deploying resource %s/%s: %w", res.GetNamespace(), res.GetName(), err)
			}

			resErr := fmt.Errorf("failure deploying resource %s/%s: %w", res.GetNamespace(), res.GetName(), err)
			logf.FromContext(ctx).Error(err, "resource deploy failed, continuing",
				"namespace", res.GetNamespace(), "name", res.GetName())

			if firstErr == nil {
				firstErr = resErr
			}

			failedResources = append(failedResources, res.GetNamespace()+"/"+res.GetName())

			continue
		}

		if ok {
			DeployedResourcesTotal.WithLabelValues(controllerName).Inc()
		}
	}

	if len(failedResources) > 0 {
		if len(failedResources) == 1 {
			return firstErr
		}

		return fmt.Errorf("%w; %d more resources failed: %s (see logs for details)",
			firstErr, len(failedResources)-1, strings.Join(failedResources[1:], ", "))
	}

	return nil
}

// ShouldSkip determines whether resource deployment should be skipped based on cache state.
func (a *Action) ShouldSkip(current *unstructured.Unstructured, desired *unstructured.Unstructured) (bool, error) {
	if desired == nil {
		return false, nil
	}

	if current != nil && !current.GetDeletionTimestamp().IsZero() {
		if a.cache != nil {
			if err := a.cache.Delete(current, desired); err != nil {
				return false, err
			}
		}
		return true, nil
	}

	if a.cache == nil {
		return false, nil
	}

	return a.cache.Has(current, desired)
}

func (a *Action) deployCRD(
	ctx context.Context,
	rr *odhTypes.ReconciliationRequest,
	obj unstructured.Unstructured,
	current *unstructured.Unstructured,
) (bool, error) {
	resources.SetLabels(&obj, a.labels)
	resources.SetAnnotations(&obj, a.annotations)

	if resources.GetLabel(&obj, a.partOfLabelKey) == "" {
		if a.partOfLabelKey == a.defaultPartOfLabelKey {
			// if a.partOfLabelDefault != "" {
			resources.SetLabel(&obj, a.partOfLabelKey, a.partOfLabelDefault)
		} else {
			fo, err := a.resolveFieldOwner(rr)
			if err != nil {
				return false, err
			}

			if fo != "" {
				resources.SetLabel(&obj, a.partOfLabelKey, fo)
			}
		}
	}

	shouldSkip, err := a.ShouldSkip(current, &obj)
	if err != nil {
		return false, err
	}
	if shouldSkip {
		return false, nil
	}

	obj = *obj.DeepCopy()
	origObj := obj.DeepCopy()

	var deployedObj *unstructured.Unstructured

	patchOps := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(resources.PlatformFieldOwner),
	}
	applyOps := []client.ApplyOption{
		client.ForceOwnership,
		client.FieldOwner(resources.PlatformFieldOwner),
	}

	switch a.deployMode {
	case ModePatch:
		deployedObj, err = a.patch(ctx, rr.Client, &obj, current, patchOps...)
	case ModeSSA:
		deployedObj, err = a.apply(ctx, rr.Client, &obj, current, applyOps...)
	default:
		err = fmt.Errorf("unsupported deploy mode %s", a.deployMode)
	}

	if err != nil {
		return false, client.IgnoreNotFound(err)
	}

	if a.cache != nil {
		err := a.cache.Add(deployedObj, origObj)
		if err != nil {
			return false, fmt.Errorf("failed to cache object: %w", err)
		}
	}

	return true, nil
}

func (a *Action) deploy(
	ctx context.Context,
	rr *odhTypes.ReconciliationRequest,
	obj unstructured.Unstructured,
	current *unstructured.Unstructured,
) (bool, error) {
	fo, err := a.resolveFieldOwner(rr)
	if err != nil {
		return false, err
	}

	resources.SetLabels(&obj, a.labels)
	resources.SetAnnotations(&obj, a.annotations)
	resources.SetAnnotation(&obj, a.annotationPrefix+metadata.SuffixInstanceGeneration, strconv.FormatInt(rr.Instance.GetGeneration(), 10))
	resources.SetAnnotation(&obj, a.annotationPrefix+metadata.SuffixInstanceName, rr.Instance.GetName())
	resources.SetAnnotation(&obj, a.annotationPrefix+metadata.SuffixInstanceUID, string(rr.Instance.GetUID()))
	resources.SetAnnotation(&obj, a.annotationPrefix+metadata.SuffixType, string(rr.Release.Name))
	resources.SetAnnotation(&obj, a.annotationPrefix+metadata.SuffixVersion, rr.Release.Version.String())

	if resources.GetLabel(&obj, a.partOfLabelKey) == "" && fo != "" {
		resources.SetLabel(&obj, a.partOfLabelKey, fo)
	}

	shouldSkip, err := a.ShouldSkip(current, &obj)
	if err != nil {
		return false, err
	}
	if shouldSkip {
		return false, nil
	}

	obj = *obj.DeepCopy()
	origObj := obj.DeepCopy()

	var deployedObj *unstructured.Unstructured

	switch {
	case resources.GetAnnotation(&obj, a.managedByAnnotation) == "false":
		resources.RemoveAnnotation(&obj, a.managedByAnnotation)

		deployedObj, err = a.create(ctx, rr.Client, &obj)
		if err != nil && !k8serr.IsAlreadyExists(err) {
			return false, err
		}

	default:
		owned := a.shouldOwn(rr, obj.GroupVersionKind())
		if owned {
			obj.SetOwnerReferences(nil)

			if err := ctrl.SetControllerReference(rr.Instance, &obj, rr.Client.Scheme()); err != nil {
				return false, err
			}
		}

		patchOps := []client.PatchOption{
			client.ForceOwnership,
			client.FieldOwner(fo),
		}
		applyOps := []client.ApplyOption{
			client.ForceOwnership,
			client.FieldOwner(fo),
		}

		switch a.deployMode {
		case ModePatch:
			deployedObj, err = a.patch(ctx, rr.Client, &obj, current, patchOps...)
		case ModeSSA:
			deployedObj, err = a.apply(ctx, rr.Client, &obj, current, applyOps...)
		default:
			err = fmt.Errorf("unsupported deploy mode %s", a.deployMode)
		}

		if err != nil {
			return false, err
		}
	}

	if a.cache != nil {
		err := a.cache.Add(deployedObj, origObj)
		if err != nil {
			return false, fmt.Errorf("failed to cache object: %w", err)
		}
	}

	return true, nil
}

func (a *Action) create(
	ctx context.Context,
	cli client.Client,
	obj *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	logf.FromContext(ctx).V(3).Info("create",
		"gvk", obj.GroupVersionKind(),
		"name", client.ObjectKeyFromObject(obj),
	)

	err := cli.Create(ctx, obj)
	if err != nil {
		return obj, err
	}

	return obj, nil
}

func (a *Action) patch(
	ctx context.Context,
	cli client.Client,
	obj *unstructured.Unstructured,
	old *unstructured.Unstructured,
	opts ...client.PatchOption,
) (*unstructured.Unstructured, error) {
	logf.FromContext(ctx).V(3).Info("patch",
		"gvk", obj.GroupVersionKind(),
		"name", client.ObjectKeyFromObject(obj),
	)

	if customizer, ok := a.patchCustomizers[obj.GroupVersionKind()]; ok {
		if err := customizer(ctx, cli, a, obj, old); err != nil {
			return nil, fmt.Errorf("patch customizer failed for %s %s/%s: %w", obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}

	if old == nil {
		err := cli.Create(ctx, obj)
		if err != nil {
			return nil, fmt.Errorf("failed to create object %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	} else {
		data, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}

		err = cli.Patch(
			ctx,
			old,
			client.RawPatch(types.ApplyPatchType, data),
			opts...,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to patch object %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	}

	return old, nil
}

func (a *Action) apply(
	ctx context.Context,
	cli client.Client,
	obj *unstructured.Unstructured,
	old *unstructured.Unstructured,
	opts ...client.ApplyOption,
) (*unstructured.Unstructured, error) {
	logf.FromContext(ctx).V(3).Info("apply",
		"gvk", obj.GroupVersionKind(),
		"name", client.ObjectKeyFromObject(obj),
	)

	if customizer, ok := a.applyCustomizers[obj.GroupVersionKind()]; ok {
		if err := customizer(ctx, cli, a, obj, old); err != nil {
			return nil, fmt.Errorf("apply customizer failed for %s %s/%s: %w", obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}

	err := resources.Apply(ctx, cli, obj, opts...)
	if err != nil {
		return nil, fmt.Errorf("apply failed %s: %w", obj.GroupVersionKind(), err)
	}

	return obj, nil
}

func (a *Action) shouldOwn(rr *odhTypes.ReconciliationRequest, objGVK schema.GroupVersionKind) bool {
	if rr.Controller.Owns(objGVK) {
		return true
	}

	if rr.Controller.IsExcludedFromDynamicOwnership(objGVK) {
		return false
	}

	if rr.Controller.IsDynamicOwnershipEnabled() {
		rr.Controller.AddDynamicOwnedType(objGVK)
		return true
	}

	return false
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		deployMode:            ModeSSA,
		partOfLabelKey:        DefaultPartOfLabelKey,
		defaultPartOfLabelKey: DefaultPartOfLabelKey,
		partOfLabelDefault:    DefaultPartOfLabelValue,
		annotationPrefix:      DefaultAnnotationPrefix,
		managedByAnnotation:   DefaultManagedByAnnotation,
		applyCustomizers: map[schema.GroupVersionKind]CustomizerFn{
			gvk.Deployment:             CustomizerFnApplyDeployments,
			gvk.ClusterRole:            CustomizerFnApplyClusterRoles,
			gvk.MonitoringStack:        CustomizerFnApplyObservability,
			gvk.TempoStack:             CustomizerFnApplyObservability,
			gvk.TempoMonolithic:        CustomizerFnApplyObservability,
			gvk.OpenTelemetryCollector: CustomizerFnApplyObservability,
		},
		patchCustomizers: map[schema.GroupVersionKind]CustomizerFn{
			gvk.Deployment: CustomizerFnPatchDeployments,
		},
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

func CustomizerFnApplyDeployments(ctx context.Context, cli client.Client, action *Action, obj *unstructured.Unstructured, old *unstructured.Unstructured) error {
	// For deployments, we allow the user to change some parameters, such as
	// container resources and replicas except:
	// - If the resource does not exist (the resource must be created)
	// - If the resource is forcefully marked as managed by the operator via
	//   annotations (i.e. to bring it back to the default values)
	if old == nil {
		return nil
	}

	if resources.GetAnnotation(old, action.managedByAnnotation) == "true" {
		// When explicitly managed, conditionally apply Strategic Merge Patch to remove user modifications.
		// Only patches when drift is detected: manifest and deployed values differ for resources or replicas.
		// NOTE: Strategic Merge Patch clears user-owned fields that SSA cannot remove, then SSA (line 500) applies manifest with operator ownership.
		if err := RevertManagedDeploymentDrift(ctx, cli, obj, old); err != nil {
			return fmt.Errorf("failed to prepare managed Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
		return nil
	}

	// To preserve backward compatibility with the current model, fields are being
	// merged from an existing Deployment (if it exists) to the rendered manifest,
	// hence the current value is preserved [1].
	//
	// Ideally deployed resources should be configured only via the platform API
	//
	// [1] https://kubernetes.io/docs/reference/using-api/server-side-apply/#conflicts
	if err := MergeDeployments(old, obj); err != nil {
		return fmt.Errorf("failed to merge Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return nil
}

func CustomizerFnApplyClusterRoles(ctx context.Context, cli client.Client, action *Action, obj *unstructured.Unstructured, old *unstructured.Unstructured) error {
	// For ClusterRole, if AggregationRule is set, then the Rules are controller managed
	// and direct changes to Rules will be stomped by the controller. This also happen if
	// the rules are set to an empty slice or nil hence we are removing the rules field
	// if the ClusterRole is set to be an aggregation role.
	_, found, err := unstructured.NestedFieldNoCopy(obj.Object, "aggregationRule")
	if err != nil {
		return err
	}
	if found {
		unstructured.RemoveNestedField(obj.Object, "rules")
	}
	return nil
}

func CustomizerFnApplyObservability(ctx context.Context, cli client.Client, action *Action, obj *unstructured.Unstructured, old *unstructured.Unstructured) error {
	if old == nil || resources.GetAnnotation(old, action.managedByAnnotation) == "true" {
		return nil
	}
	if err := MergeObservabilityResources(old, obj); err != nil {
		return fmt.Errorf("failed to merge %s %s/%s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
	}
	return nil
}

func CustomizerFnPatchDeployments(ctx context.Context, cli client.Client, action *Action, obj *unstructured.Unstructured, old *unstructured.Unstructured) error {
	if old == nil {
		return nil
	}

	if resources.GetAnnotation(old, action.managedByAnnotation) == "true" {
		if err := RevertManagedDeploymentDrift(ctx, cli, obj, old); err != nil {
			return fmt.Errorf("failed to prepare managed Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
		return nil
	}

	if err := RemoveDeploymentsResources(obj); err != nil {
		return fmt.Errorf("failed to apply allow list to Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}

	return nil
}
