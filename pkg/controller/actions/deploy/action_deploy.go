package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
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

func (a *Action) run(ctx context.Context, rr *odhTypes.ReconciliationRequest) error {
	for i := range rr.Resources {
		obj := rr.Resources[i]

		current, lookupErr := a.lookup(ctx, rr.Client, obj)
		if lookupErr != nil && !k8serr.IsNotFound(lookupErr) {
			return fmt.Errorf("failed to lookup object %s/%s: %w", obj.GetNamespace(), obj.GetName(), lookupErr)
		}

		resources.SetLabels(&obj, a.labels)
		resources.SetAnnotations(&obj, a.annotations)

		resources.SetAnnotation(&obj, annotations.ComponentGeneration, strconv.FormatInt(rr.Instance.GetGeneration(), 10))
		resources.SetAnnotation(&obj, annotations.PlatformType, string(rr.Release.Name))
		resources.SetAnnotation(&obj, annotations.PlatformVersion, rr.Release.Version.String())

		switch {
		// the user has explicitly marked the current object as not owned by the operator, so
		// skip any further processing
		case current != nil && resources.GetAnnotation(current, annotations.ManagedByODHOperator) == "false":
			continue

		// The object is explicitly marked as not owned by the operator in the manifests,
		// so it should be created if it doesn't exist, but should not be modified afterward.
		case resources.GetAnnotation(&obj, annotations.ManagedByODHOperator) == "false":
			// remove the opendatahub.io/managed as it should not be set
			// to the actual object in this case
			resources.RemoveAnnotation(&obj, annotations.ManagedByODHOperator)

			err := a.create(ctx, rr.Client, obj)
			if err != nil {
				return err
			}

		default:
			owned := rr.Manager.Owns(obj.GroupVersionKind())
			if owned {
				if err := ctrl.SetControllerReference(rr.Instance, &obj, rr.Client.Scheme()); err != nil {
					return err
				}
			}

			var err error

			switch a.deployMode {
			case ModePatch:
				err = a.patch(ctx, rr.Client, obj, current)
			case ModeSSA:
				err = a.apply(ctx, rr.Client, obj, current)
			default:
				err = fmt.Errorf("unsupported deploy mode %s", a.deployMode)
			}

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (a *Action) lookup(ctx context.Context, c *odhClient.Client, obj unstructured.Unstructured) (*unstructured.Unstructured, error) {
	found := unstructured.Unstructured{}
	found.SetGroupVersionKind(obj.GroupVersionKind())

	// TODO: use PartialObjectMetadata for resources where it make sense
	err := c.Get(ctx, client.ObjectKeyFromObject(&obj), &found)
	if err != nil {
		return nil, err
	}

	return &found, nil
}

func (a *Action) create(ctx context.Context, c *odhClient.Client, obj unstructured.Unstructured) error {
	logf.FromContext(ctx).V(3).Info("create",
		"gvk", obj.GroupVersionKind(),
		"name", client.ObjectKeyFromObject(&obj),
	)

	err := c.Create(ctx, &obj)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (a *Action) patch(ctx context.Context, c *odhClient.Client, obj unstructured.Unstructured, old *unstructured.Unstructured) error {
	logf.FromContext(ctx).V(3).Info("patch",
		"gvk", obj.GroupVersionKind(),
		"name", client.ObjectKeyFromObject(&obj),
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
		if err := RemoveDeploymentsResources(&obj); err != nil {
			return fmt.Errorf("failed to apply allow list to Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}

	default:
		// do noting
		break
	}

	if old == nil {
		err := c.Create(ctx, &obj)
		if err != nil {
			return fmt.Errorf("failed to create object %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	} else {
		data, err := json.Marshal(obj)
		if err != nil {
			return err
		}

		err = c.Patch(
			ctx,
			old,
			client.RawPatch(types.ApplyPatchType, data),
			client.ForceOwnership,
			client.FieldOwner(a.fieldOwner),
		)

		if err != nil {
			return fmt.Errorf("failed to patch object %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	}

	return nil
}

func (a *Action) apply(ctx context.Context, c *odhClient.Client, obj unstructured.Unstructured, old *unstructured.Unstructured) error {
	logf.FromContext(ctx).V(3).Info("apply",
		"gvk", obj.GroupVersionKind(),
		"name", client.ObjectKeyFromObject(&obj),
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
		if err := MergeDeployments(old, &obj); err != nil {
			return fmt.Errorf("failed to merge Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	default:
		// do noting
		break
	}

	err := c.Apply(
		ctx,
		&obj,
		client.ForceOwnership,
		client.FieldOwner(a.fieldOwner),
	)

	if err != nil {
		return fmt.Errorf("apply failed %s: %w", obj.GroupVersionKind(), err)
	}

	return nil
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
