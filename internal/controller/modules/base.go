package modules

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manifests/kustomize"
)

// ModuleConfig holds the static, declarative metadata for a module.
// Module teams populate this struct; BaseHandler provides default
// implementations of ModuleHandler methods that operate on it.
//
// Set either ChartDir (Helm) or ManifestDir (Kustomize) to select the
// manifest format. If both are set, both are returned.
type ModuleConfig struct {
	// Name is the unique identifier for this module (used as registry key).
	Name string

	// GVK is the GroupVersionKind of the module CR managed by this handler.
	GVK schema.GroupVersionKind

	// CRName is the singleton name of the module CR instance (e.g. "default").
	CRName string

	// Helm fields -- used when ChartDir is set.

	// ReleaseName is the Helm release name for the module operator chart.
	ReleaseName string

	// ChartDir is the chart directory name relative to DefaultChartsPath.
	ChartDir string

	// Values are additional Helm values passed when rendering the chart.
	Values map[string]any

	// NamespaceValueKey is the Helm value key used to set the operator
	// deployment namespace (e.g. "operatorNamespace", "namespace"). When
	// set, BaseHandler.GetOperatorManifests injects
	// platform.ApplicationsNamespace under this key. Leave empty if the
	// chart does not need a namespace override.
	NamespaceValueKey string

	// Kustomize fields -- used when ManifestDir is set.

	// ManifestDir is the directory name relative to rr.ManifestsBasePath
	// containing Kustomize overlays for the module operator.
	ManifestDir string

	// ContextDir is an optional subdirectory within ManifestDir.
	ContextDir string

	// SourcePath is an optional overlay path within ContextDir.
	SourcePath string

	// Namespace overrides the default ApplicationsNamespace for Kustomize
	// rendering. When empty, Kustomize uses ApplicationsNamespace. Set this
	// for modules that deploy into a dedicated namespace. For Helm modules,
	// use NamespaceValueKey or Values instead; this field is not wired into
	// Helm rendering.
	Namespace string

	// RelatedImages lists RELATED_IMAGE_* environment variable names that the
	// module operator needs. The platform reads each name from its own process
	// environment (where the release pipeline sets digest-pinned references)
	// and injects them into the module operator's Deployment before apply.
	// Variables whose values are empty on the platform operator are skipped.
	RelatedImages []string
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

func (b *BaseHandler) GetRelatedImages() []string {
	return b.Config.RelatedImages
}

func (b *BaseHandler) GetOperatorManifests(platform *PlatformContext) OperatorManifests {
	var result OperatorManifests

	if b.Config.ChartDir != "" && platform != nil {
		vals := make(map[string]any, len(b.Config.Values))
		for k, v := range b.Config.Values {
			vals[k] = v
		}

		if b.Config.NamespaceValueKey != "" && platform.ApplicationsNamespace != "" {
			vals[b.Config.NamespaceValueKey] = platform.ApplicationsNamespace
		}

		result.HelmCharts = []types.HelmChartInfo{{
			Source: helm.Source{
				Chart:       filepath.Join(platform.ChartsBasePath, b.Config.ChartDir),
				ReleaseName: b.Config.ReleaseName,
				Values:      helm.Values(vals),
			},
		}}
	}

	if b.Config.ManifestDir != "" {
		result.Manifests = []types.ManifestInfo{{
			Path:       b.Config.ManifestDir,
			ContextDir: b.Config.ContextDir,
			SourcePath: b.Config.SourcePath,
			Namespace:  b.Config.Namespace,
		}}
	}

	return result
}

// GetModuleStatus reads the module CR by GVK+CRName and extracts status
// conditions and generation metadata for staleness detection.
func (b *BaseHandler) GetModuleStatus(ctx context.Context, cli client.Client) (*ModuleStatus, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(b.Config.GVK)
	u.SetName(b.Config.CRName)

	if err := cli.Get(ctx, client.ObjectKeyFromObject(u), u); err != nil {
		return nil, err
	}

	conditions, err := ParseConditions(u)
	if err != nil {
		return nil, err
	}

	observedGen, _, _ := unstructured.NestedInt64(u.Object, "status", "observedGeneration")

	return &ModuleStatus{
		Conditions:         conditions,
		ObservedGeneration: observedGen,
		Generation:         u.GetGeneration(),
	}, nil
}

// GetModuleCRState returns the lifecycle state of the module CR. It
// distinguishes between absent, alive, and being-deleted (has
// deletionTimestamp but finalizers are still being processed).
func (b *BaseHandler) GetModuleCRState(ctx context.Context, cli client.Client) (CRState, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(b.Config.GVK)

	err := cli.Get(ctx, client.ObjectKey{Name: b.Config.CRName}, u)
	if err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return CRStateAbsent, nil
		}
		return CRStateAbsent, err
	}

	if !u.GetDeletionTimestamp().IsZero() {
		return CRStateDeleting, nil
	}

	return CRStateAlive, nil
}

// DeleteModuleCR deletes the module CR from the cluster. Returns nil if the
// CR or its CRD does not exist, making the call idempotent.
func (b *BaseHandler) DeleteModuleCR(ctx context.Context, cli client.Client) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(b.Config.GVK)
	u.SetName(b.Config.CRName)

	if err := cli.Delete(ctx, u); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("deleting module CR %s/%s: %w", b.Config.GVK.Kind, b.Config.CRName, err)
	}

	logf.FromContext(ctx).Info("deleted module CR",
		"module", b.Config.Name,
		"kind", b.Config.GVK.Kind,
		"name", b.Config.CRName)

	return nil
}

// DeleteOperatorResources renders the module's operator manifests (Helm and/or
// Kustomize) and deletes each resource from the cluster. NotFound errors are
// silently ignored so the operation is idempotent.
func (b *BaseHandler) DeleteOperatorResources(ctx context.Context, cli client.Client, platform *PlatformContext) error {
	log := logf.FromContext(ctx)
	manifests := b.GetOperatorManifests(platform)

	for _, chartInfo := range manifests.HelmCharts {
		renderer, err := helm.New([]helm.Source{chartInfo.Source})
		if err != nil {
			return fmt.Errorf("creating helm renderer for %s: %w", b.Config.Name, err)
		}

		resources, err := renderer.Process(ctx, nil)
		if err != nil {
			return fmt.Errorf("rendering chart for %s: %w", b.Config.Name, err)
		}

		if err := b.deleteRenderedResources(ctx, cli, log, resources); err != nil {
			return err
		}
	}

	for _, manifestInfo := range manifests.Manifests {
		ke := kustomize.NewEngine()
		ns := ""
		if platform != nil {
			ns = platform.ApplicationsNamespace
		}
		if manifestInfo.Namespace != "" {
			ns = manifestInfo.Namespace
		}

		var renderOpts []kustomize.RenderOptsFn
		if ns != "" {
			renderOpts = append(renderOpts, kustomize.WithNamespace(ns))
		}

		resources, err := ke.Render(manifestInfo.String(), renderOpts...)
		if err != nil {
			return fmt.Errorf("rendering kustomize manifests for %s: %w", b.Config.Name, err)
		}

		if err := b.deleteRenderedResources(ctx, cli, log, resources); err != nil {
			return err
		}
	}

	return nil
}

func (b *BaseHandler) deleteRenderedResources(
	ctx context.Context,
	cli client.Client,
	log logr.Logger,
	resources []unstructured.Unstructured,
) error {
	for i := range resources {
		res := &resources[i]

		if res.GroupVersionKind() == gvk.CustomResourceDefinition {
			log.V(1).Info("skipping CRD deletion during module cleanup",
				"module", b.Config.Name,
				"name", res.GetName())
			continue
		}

		log.V(1).Info("deleting module operator resource",
			"module", b.Config.Name,
			"kind", res.GetKind(),
			"name", res.GetName(),
			"namespace", res.GetNamespace())

		if err := cli.Delete(ctx, res); err != nil {
			if !k8serr.IsNotFound(err) {
				return fmt.Errorf("deleting %s %s/%s for module %s: %w",
					res.GetKind(), res.GetNamespace(), res.GetName(), b.Config.Name, err)
			}
		}
	}

	return nil
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

		if v, ok := cm["observedGeneration"].(int64); ok {
			c.ObservedGeneration = v
		} else if v, ok := cm["observedGeneration"].(float64); ok {
			c.ObservedGeneration = int64(v)
		}

		if v, ok := cm["lastTransitionTime"].(string); ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				c.LastTransitionTime = metav1.NewTime(t)
			}
		}

		conditions = append(conditions, c)
	}

	return conditions, nil
}
