package registry

import (
	"context"
	"fmt"
	"reflect"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// InstanceNamer is an optional interface a ComponentHandler can implement
// to expose the well-known singleton CR name and GVK. The readiness
// checker uses this on xKS (where DSC is nil) to fetch the CR directly
// without calling NewCRObject, which requires a non-nil DSC.
type InstanceNamer interface {
	GetInstanceName() string
	GetInstanceGVK() schema.GroupVersionKind
}

// ComponentReadinessChecker implements dag.ReadinessChecker for
// in-tree components. It looks up the handler by name, constructs
// the expected CR object, fetches it from the cluster, and checks
// the Ready condition on the live status.
type ComponentReadinessChecker struct {
	registry *Registry
	client   client.Client
	dsc      *dscv2.DataScienceCluster
}

// NewReadinessChecker creates a ReadinessChecker backed by this
// component registry. It needs a client to fetch each component's
// CR and inspect its conditions. The dsc parameter may be nil on
// xKS where the DSC controller is suppressed; in that case the
// checker falls back to registry-level enablement and the
// InstanceNamer interface for CR lookups.
func NewReadinessChecker(reg *Registry, cli client.Client, dsc *dscv2.DataScienceCluster) *ComponentReadinessChecker {
	return &ComponentReadinessChecker{
		registry: reg,
		client:   cli,
		dsc:      dsc,
	}
}

// IsReady returns true if the named component's CR has Ready=True
// on the cluster. Disabled components and components without a CR
// are considered ready. If the CR has not been created yet (NotFound),
// the component is not ready.
func (c *ComponentReadinessChecker) IsReady(ctx context.Context, name string) (bool, error) {
	handler := c.registry.Lookup(name)
	if handler == nil {
		return false, fmt.Errorf("component %q: %w", name, dag.ErrUnknownNode)
	}

	if c.dsc == nil {
		return c.isReadyWithoutDSC(ctx, name, handler)
	}

	if !handler.IsEnabled(c.dsc) {
		return true, nil
	}

	ci, err := handler.NewCRObject(ctx, c.client, c.dsc)
	if err != nil {
		return false, err
	}
	if isNilPlatformObject(ci) {
		return true, nil
	}

	obj, ok := ci.(client.Object)
	if !ok {
		return false, fmt.Errorf("component %q CR does not implement client.Object", name)
	}

	err = c.client.Get(ctx, types.NamespacedName{Name: obj.GetName()}, obj)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("component %q: failed to get CR: %w", name, err)
	}

	live, ok := obj.(common.PlatformObject)
	if !ok {
		return false, fmt.Errorf("component %q CR does not implement PlatformObject", name)
	}

	return isComponentCRReady(live), nil
}

func (c *ComponentReadinessChecker) isReadyWithoutDSC(ctx context.Context, name string, handler ComponentHandler) (bool, error) {
	if !c.registry.IsEnabled(name) {
		return true, nil
	}

	namer, ok := handler.(InstanceNamer)
	if !ok {
		return true, nil
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(namer.GetInstanceGVK())

	if err := c.client.Get(ctx, types.NamespacedName{Name: namer.GetInstanceName()}, u); err != nil {
		if k8serr.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("component %q: failed to get CR: %w", name, err)
	}

	return isUnstructuredCRReady(u), nil
}

func isUnstructuredCRReady(u *unstructured.Unstructured) bool {
	conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	if !found {
		return false
	}
	for _, item := range conditions {
		c, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(c, "type")
		condStatus, _, _ := unstructured.NestedString(c, "status")
		if condType == status.ConditionTypeReady {
			return condStatus == string(metav1.ConditionTrue)
		}
	}
	return false
}

func isComponentCRReady(obj common.PlatformObject) bool {
	s := obj.GetStatus()
	if s == nil {
		return false
	}
	for _, c := range s.Conditions {
		if c.Type == status.ConditionTypeReady {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

func isNilPlatformObject(v any) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil())
}
