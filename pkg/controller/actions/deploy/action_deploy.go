package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type Mode string

const (
	ModePatch Mode = "patch"
	ModeSSA   Mode = "ssa"
)

// Action deploys the resources that are included in the ReconciliationRequest using
// the same create or patch machinery implemented as part of deploy.DeployManifestsFromPath.
type Action struct {
	fieldOwner  string
	deployMode  Mode
	labels      map[string]string
	annotations map[string]string
	cache       *Cache
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

		for k, v := range values {
			action.labels[k] = v
		}
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

		for k, v := range values {
			action.annotations[k] = v
		}
	}
}

func WithCache(opts ...CacheOpt) ActionOpts {
	return func(action *Action) {
		action.cache = NewCache(opts...)
	}
}

func (a *Action) run(ctx context.Context, rr *odhTypes.ReconciliationRequest) error {
	// cleanup old entries if needed
	if a.cache != nil {
		a.cache.Sync()
	}

	kind, err := resources.KindForObject(rr.Client.Scheme(), rr.Instance)
	if err != nil {
		return err
	}

	controllerName := strings.ToLower(kind)
	igvk := rr.Instance.GetObjectKind().GroupVersionKind()

	for i := range rr.Resources {
		res := rr.Resources[i]
		current := resources.GvkToUnstructured(res.GroupVersionKind())

		lookupErr := rr.Client.Get(ctx, client.ObjectKeyFromObject(&res), current)
		switch {
		case k8serr.IsNotFound(lookupErr):
			// set it to nil fto pass it down to other methods and signal
			// that there's no previous known state of the resource
			current = nil
		case lookupErr != nil:
			return fmt.Errorf("failed to lookup object %s/%s: %w", res.GetNamespace(), res.GetName(), lookupErr)
		default:
			// Remove the previous owner reference if set, This is required during the
			// transition from the old to the new operator.
			if err := resources.RemoveOwnerReferences(ctx, rr.Client, current, ownedTypeIsNot(&igvk)); err != nil {
				return err
			}

			// the user has explicitly marked the current object as not owned by the operator
			if resources.GetAnnotation(current, annotations.ManagedByODHOperator) == "false" {
				// de-own the object so the resource is not removed upon cleanup
				if err := resources.RemoveOwnerReferences(ctx, rr.Client, current, ownedTypeIs(&igvk)); err != nil {
					return err
				}

				//  skip any further processing
				continue
			}
		}

		var ok bool
		var err error

		switch rr.Resources[i].GroupVersionKind() {
		case gvk.CustomResourceDefinition:
			ok, err = a.deployCRD(ctx, rr, res, current)
		default:
			ok, err = a.deploy(ctx, rr, res, current)
		}

		if err != nil {
			return fmt.Errorf("failure deploying resource %s: %w", res, err)
		}

		if ok {
			DeployedResourcesTotal.WithLabelValues(controllerName).Inc()
		}
	}

	return nil
}

// ShouldSkip determines whether resource deployment should be skipped based on cache state.
// Returns true if the resource is cached and deployment should be skipped, false if deployment should proceed.
// Delegates to cache for deletion timestamp handling and cache cleanup.
func (a *Action) ShouldSkip(current *unstructured.Unstructured, desired *unstructured.Unstructured) (bool, error) {
	// Defensive: desired is expected non-nil; proceed if misuse happens.
	if desired == nil {
		return false, nil
	}

	// Skip deployment if current object is terminating
	// The action will be re-triggered once the object gets deleted
	if current != nil && !current.GetDeletionTimestamp().IsZero() {
		// Clean up cache if configured
		if a.cache != nil {
			if err := a.cache.Delete(current, desired); err != nil {
				return false, err
			}
		}
		return true, nil // skip deployment
	}

	// Always proceed if no cache configured
	if a.cache == nil {
		return false, nil
	}

	// Return normal cache decision for non-terminating objects
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
	resources.SetLabel(&obj, labels.PlatformPartOf, labels.Platform)

	shouldSkip, err := a.ShouldSkip(current, &obj)
	if err != nil {
		return false, err
	}
	if shouldSkip {
		return false, nil
	}

	// backup copy for caching
	origObj := obj.DeepCopy()

	var deployedObj *unstructured.Unstructured

	ops := []client.PatchOption{
		client.ForceOwnership,
		// Since CRDs are not bound to a component, set the field
		// owner to the platform itself
		client.FieldOwner(resources.PlatformFieldOwner),
	}

	switch a.deployMode {
	case ModePatch:
		deployedObj, err = a.patch(ctx, rr.Client, &obj, current, ops...)
	case ModeSSA:
		deployedObj, err = a.apply(ctx, rr.Client, &obj, current, ops...)
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
	fo := a.fieldOwner
	if fo == "" {
		kind, err := resources.KindForObject(rr.Client.Scheme(), rr.Instance)
		if err != nil {
			return false, err
		}

		fo = strings.ToLower(kind)
	}

	resources.SetLabels(&obj, a.labels)
	resources.SetAnnotations(&obj, a.annotations)
	resources.SetAnnotation(&obj, annotations.InstanceGeneration, strconv.FormatInt(rr.Instance.GetGeneration(), 10))
	resources.SetAnnotation(&obj, annotations.InstanceName, rr.Instance.GetName())
	resources.SetAnnotation(&obj, annotations.InstanceUID, string(rr.Instance.GetUID()))
	resources.SetAnnotation(&obj, annotations.PlatformType, string(rr.Release.Name))
	resources.SetAnnotation(&obj, annotations.PlatformVersion, rr.Release.Version.String())

	if resources.GetLabel(&obj, labels.PlatformPartOf) == "" && fo != "" {
		resources.SetLabel(&obj, labels.PlatformPartOf, fo)
	}

	shouldSkip, err := a.ShouldSkip(current, &obj)
	if err != nil {
		return false, err
	}
	if shouldSkip {
		return false, nil
	}

	// backup copy for caching
	origObj := obj.DeepCopy()

	var deployedObj *unstructured.Unstructured

	switch {
	// The object is explicitly marked as not owned by the operator in the manifests,
	// so it should be created if it doesn't exist, but should not be modified afterward.
	case resources.GetAnnotation(&obj, annotations.ManagedByODHOperator) == "false":
		// remove the opendatahub.io/managed as it should not be set
		// to the actual object in this case
		resources.RemoveAnnotation(&obj, annotations.ManagedByODHOperator)

		deployedObj, err = a.create(ctx, rr.Client, &obj)
		if err != nil && !k8serr.IsAlreadyExists(err) {
			return false, err
		}

	default:
		owned := rr.Controller.Owns(obj.GroupVersionKind())
		if owned {
			if err := ctrl.SetControllerReference(rr.Instance, &obj, rr.Client.Scheme()); err != nil {
				return false, err
			}
		}

		ops := []client.PatchOption{
			client.ForceOwnership,
			client.FieldOwner(fo),
		}

		switch a.deployMode {
		case ModePatch:
			deployedObj, err = a.patch(ctx, rr.Client, &obj, current, ops...)
		case ModeSSA:
			deployedObj, err = a.apply(ctx, rr.Client, &obj, current, ops...)
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

	switch obj.GroupVersionKind() {
	case gvk.Deployment:
		// For deployments, we allow the user to change some parameters, such as
		// container resources and replicas except:
		// - If the resource does not exist (the resource must be created)
		// - If the resource is forcefully marked as managed by the operator via
		//   annotations (i.e. to bring it back to the default values)
		if old == nil || resources.GetAnnotation(old, annotations.ManagedByODHOperator) == "true" {
			break
		}

		// To preserve backward compatibility with the current model, fields are being
		// removed, hence not included in the final PATCH. Ideally with should leverage
		// Server-Side Apply.
		//
		// Ideally deployed resources should be configured only via the platform API
		if err := RemoveDeploymentsResources(obj); err != nil {
			return nil, fmt.Errorf("failed to apply allow list to Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	default:
		// do nothing
		break
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
	opts ...client.PatchOption,
) (*unstructured.Unstructured, error) {
	logf.FromContext(ctx).V(3).Info("apply",
		"gvk", obj.GroupVersionKind(),
		"name", client.ObjectKeyFromObject(obj),
	)

	switch obj.GroupVersionKind() {
	case gvk.Deployment:
		// For deployments, we allow the user to change some parameters, such as
		// container resources and replicas except:
		// - If the resource does not exist (the resource must be created)
		// - If the resource is forcefully marked as managed by the operator via
		//   annotations (i.e. to bring it back to the default values)
		if old == nil || resources.GetAnnotation(old, annotations.ManagedByODHOperator) == "true" {
			break
		}

		// To preserve backward compatibility with the current model, fields are being
		// merged from an existing Deployment (if it exists) to the rendered manifest,
		// hence the current value is preserved [1].
		//
		// Ideally deployed resources should be configured only via the platform API
		//
		// [1] https://kubernetes.io/docs/reference/using-api/server-side-apply/#conflicts
		if err := MergeDeployments(old, obj); err != nil {
			return nil, fmt.Errorf("failed to merge Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	case gvk.ClusterRole:
		// For ClusterRole, if AggregationRule is set, then the Rules are controller managed
		// and direct changes to Rules will be stomped by the controller. This also happen if
		// the rules are set to an empty slice or nil hence we are removing the rules field
		// if the ClusterRole is set to be an aggregation role.
		_, found, err := unstructured.NestedFieldNoCopy(obj.Object, "aggregationRule")
		if err != nil {
			return nil, err
		}
		if found {
			unstructured.RemoveNestedField(obj.Object, "rules")
		}
	// For observability components, we preserve user changes to resource requests/limits
	case gvk.MonitoringStack, gvk.TempoStack, gvk.TempoMonolithic, gvk.OpenTelemetryCollector:
		if old == nil || resources.GetAnnotation(old, annotations.ManagedByODHOperator) == "true" {
			break
		}
		if err := MergeObservabilityResources(old, obj); err != nil {
			return nil, fmt.Errorf("failed to merge %s %s/%s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	default:
		// do nothing
		break
	}

	err := resources.Apply(ctx, cli, obj, opts...)
	if err != nil {
		return nil, fmt.Errorf("apply failed %s: %w", obj.GroupVersionKind(), err)
	}

	return obj, nil
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		deployMode: ModeSSA,
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}
