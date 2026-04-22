package modules

import (
	"context"
	"path/filepath"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// ModuleConfig holds the static, declarative metadata for a module.
// Module teams populate this struct; BaseHandler provides default
// implementations of ModuleHandler methods that operate on it.
type ModuleConfig struct {
	// Name is the unique identifier for this module (used as registry key).
	Name string

	// GVK is the GroupVersionKind of the module CR managed by this handler.
	GVK schema.GroupVersionKind

	// CRName is the singleton name of the module CR instance (e.g. "default").
	CRName string

	// ReleaseName is the Helm release name for the module operator chart.
	ReleaseName string

	// ChartDir is the chart directory name relative to DefaultChartsPath.
	ChartDir string

	// Values are additional Helm values passed when rendering the chart.
	Values map[string]any
}

// BaseHandler provides default implementations for ModuleHandler methods
// that are purely mechanical. Module teams embed this struct and only
// override IsEnabled and BuildModuleCR.
type BaseHandler struct {
	Config ModuleConfig
}

func (b *BaseHandler) GetName() string {
	return b.Config.Name
}

func (b *BaseHandler) GetGVK() schema.GroupVersionKind {
	return b.Config.GVK
}

func (b *BaseHandler) GetOperatorCharts() []types.HelmChartInfo {
	vals := b.Config.Values
	if vals == nil {
		vals = map[string]any{}
	}

	return []types.HelmChartInfo{{
		Source: helm.Source{
			Chart:       filepath.Join(ccmcommon.DefaultChartsPath, b.Config.ChartDir),
			ReleaseName: b.Config.ReleaseName,
			Values:      helm.Values(vals),
		},
	}}
}

// GetModuleStatus reads the module CR by GVK+CRName and extracts
// .status.conditions as []metav1.Condition.
func (b *BaseHandler) GetModuleStatus(ctx context.Context, cli client.Client) ([]metav1.Condition, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(b.Config.GVK)
	u.SetName(b.Config.CRName)

	if err := cli.Get(ctx, client.ObjectKeyFromObject(u), u); err != nil {
		return nil, err
	}

	return ParseConditions(u)
}

// ParseConditions extracts []metav1.Condition from an unstructured object's
// .status.conditions field.
func ParseConditions(u *unstructured.Unstructured) ([]metav1.Condition, error) {
	rawConditions, found, err := unstructured.NestedSlice(u.Object, "status", "conditions")
	if err != nil {
		return nil, err
	}

	if !found {
		return nil, nil
	}

	conditions := make([]metav1.Condition, 0, len(rawConditions))

	for _, raw := range rawConditions {
		cm, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		c := metav1.Condition{}

		if v, ok := cm["type"].(string); ok {
			c.Type = v
		}

		if v, ok := cm["status"].(string); ok {
			c.Status = metav1.ConditionStatus(v)
		}

		if v, ok := cm["reason"].(string); ok {
			c.Reason = v
		}

		if v, ok := cm["message"].(string); ok {
			c.Message = v
		}

		conditions = append(conditions, c)
	}

	return conditions, nil
}
