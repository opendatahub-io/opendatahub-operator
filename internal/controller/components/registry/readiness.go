package registry

import (
	"context"
	"fmt"
	"reflect"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

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
// component registry. It needs a client and DSC instance to fetch
// each component's CR and inspect its conditions.
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

	// Fetch the live CR from the cluster to read its actual status.
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
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr && reflect.ValueOf(v).IsNil())
}
