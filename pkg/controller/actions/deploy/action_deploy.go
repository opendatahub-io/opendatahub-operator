package deploy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
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

func WithCache() ActionOpts {
	return func(action *Action) {
		action.cache = newCache()
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

	for i := range rr.Resources {
		ok, err := a.deploy(ctx, rr, rr.Resources[i])
		if err != nil {
			return fmt.Errorf("failure deploying %s: %w", rr.Resources[i], err)
		}

		if ok {
			DeployedResourcesTotal.WithLabelValues(controllerName).Inc()
		}
	}

	return nil
}

func (a *Action) deploy(
	ctx context.Context,
	rr *odhTypes.ReconciliationRequest,
	obj unstructured.Unstructured,
) (bool, error) {
	current, lookupErr := a.lookup(ctx, rr.Client, obj)
	if lookupErr != nil {
		return false, fmt.Errorf("failed to lookup object %s/%s: %w", obj.GetNamespace(), obj.GetName(), lookupErr)
	}

	// the user has explicitly marked the current object as not owned by the operator, so
	// skip any further processing
	if current != nil && resources.GetAnnotation(current, annotations.ManagedByODHOperator) == "false" {
		return false, nil
	}

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

	// backup copy for caching
	origObj := obj.DeepCopy()

	if a.cache != nil && current != nil {
		ck, err := a.computeCacheKey(current, &obj)
		if err != nil {
			return false, fmt.Errorf("failed to compute request identifier: %w", err)
		}

		// no changes, no need to re-deploy it
		if a.cache.Has(ck) {
			return false, nil
		}
	}

	var deployedObj *unstructured.Unstructured
	var deployed bool

	switch {
	// The object is explicitly marked as not owned by the operator in the manifests,
	// so it should be created if it doesn't exist, but should not be modified afterward.
	case resources.GetAnnotation(&obj, annotations.ManagedByODHOperator) == "false":
		// remove the opendatahub.io/managed as it should not be set
		// to the actual object in this case
		resources.RemoveAnnotation(&obj, annotations.ManagedByODHOperator)

		var err error

		deployedObj, deployed, err = a.create(ctx, rr.Client, &obj)
		if err != nil {
			return false, err
		}

	default:
		owned := rr.Manager.Owns(obj.GroupVersionKind())
		if owned {
			if err := ctrl.SetControllerReference(rr.Instance, &obj, rr.Client.Scheme()); err != nil {
				return false, err
			}
		}

		var err error

		switch a.deployMode {
		case ModePatch:
			deployedObj, err = a.patch(
				ctx,
				rr.Client,
				&obj,
				current,
				client.ForceOwnership,
				client.FieldOwner(fo))
		case ModeSSA:
			deployedObj, err = a.apply(
				ctx,
				rr.Client,
				&obj,
				current,
				client.ForceOwnership,
				client.FieldOwner(fo))
		default:
			err = fmt.Errorf("unsupported deploy mode %s", a.deployMode)
		}

		if err != nil {
			return false, err
		}

		deployed = true
	}

	// If the deployment did not update the object, add the request to the cache.
	if a.cache != nil {
		ck, err := a.computeCacheKey(deployedObj, origObj)
		if err != nil {
			return false, fmt.Errorf("failed to compute request identifier after apply: %w", err)
		}

		a.cache.Add(ck)
	}

	return deployed, nil
}

func (a *Action) lookup(
	ctx context.Context,
	c *odhClient.Client,
	obj unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	found := unstructured.Unstructured{}
	found.SetGroupVersionKind(obj.GroupVersionKind())

	// TODO: use PartialObjectMetadata for resources where it make sense
	err := c.Get(ctx, client.ObjectKeyFromObject(&obj), &found)
	switch {
	case err == nil:
		return &found, nil
	case k8serr.IsNotFound(err):
		return nil, nil
	default:
		return nil, err
	}
}

func (a *Action) create(
	ctx context.Context,
	c *odhClient.Client,
	obj *unstructured.Unstructured,
) (*unstructured.Unstructured, bool, error) {
	logf.FromContext(ctx).V(3).Info("create",
		"gvk", obj.GroupVersionKind(),
		"name", client.ObjectKeyFromObject(obj),
	)

	err := c.Create(ctx, obj)
	switch {
	case err == nil:
		return obj, true, nil
	case k8serr.IsAlreadyExists(err):
		return obj, false, nil
	default:
		return nil, false, err
	}
}

func (a *Action) patch(
	ctx context.Context,
	c *odhClient.Client,
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
		err := c.Create(ctx, obj)
		if err != nil {
			return nil, fmt.Errorf("failed to create object %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	} else {
		data, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}

		err = c.Patch(
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
	c *odhClient.Client,
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
	default:
		// do nothing
		break
	}

	err := c.Apply(
		ctx,
		obj,
		opts...,
	)

	if err != nil {
		return nil, fmt.Errorf("apply failed %s: %w", obj.GroupVersionKind(), err)
	}

	return obj, nil
}

func (a *Action) computeCacheKey(
	original *unstructured.Unstructured,
	modified *unstructured.Unstructured,
) (string, error) {
	modifiedObjectHash, err := resources.Hash(modified)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s.%s.%s.%s.%s",
		original.GroupVersionKind().GroupVersion(),
		original.GroupVersionKind().Kind,
		klog.KObj(original),
		original.GetResourceVersion(),
		base64.RawURLEncoding.EncodeToString(modifiedObjectHash),
	), nil
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
