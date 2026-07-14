package modules

import (
	"context"
	"fmt"
	"maps"
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

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
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

	// SourcePathByPlatform selects the overlay path (relative to ManifestDir)
	// per platform flavor (e.g. overlays/odh, overlays/rhoai). When set and the
	// active platform has an entry, it takes precedence over SourcePath.
	SourcePathByPlatform map[common.Platform]string

	// Namespace overrides the default ApplicationsNamespace for Kustomize
	// rendering. When empty, Kustomize uses ApplicationsNamespace. Set this
	// for modules that deploy into a dedicated namespace. For Helm modules,
	// use NamespaceValueKey or Values instead; this field is not wired into
	// Helm rendering.
	Namespace string

	// ContainerName is the name of the primary operator container in the
	// module's Deployment. Defaults to "manager" (the kubebuilder convention).
	// Override only if the module chart uses a different container name.
	ContainerName string

	// DeploymentName is the metadata.name of the module operator's Deployment
	// as rendered (after any kustomize namePrefix/nameSuffix or Helm release
	// templating). It is the Deployment the platform injects RELATED_IMAGE_*
	// env vars into. When empty, the platform falls back to the Helm release
	// name (Helm modules) or the module name (kustomize modules). Set this for
	// kustomize modules whose rendered Deployment name differs from the module
	// name, otherwise env injection silently targets the wrong name.
	DeploymentName string

	// ControllerImage is the RELATED_IMAGE_* env var name whose value is the
	// fully-qualified image reference for this module's operator container.
	// When set and the env var is present on the platform operator process,
	// the inject action overwrites the target container's image field in the
	// rendered Deployment. Leave empty if the chart already bakes in the
	// correct image and no override is needed.
	ControllerImage string

	// InitContainerName is the name of an init container whose image field
	// should be overridden with the same ControllerImage value. This is for
	// modules whose Deployment includes an init container that shares the
	// operator image (e.g. "copy-manifests"). Leave empty if no init
	// container needs the override.
	InitContainerName string

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

func (b *BaseHandler) GetReadyConditionType() string {
	return b.Config.GVK.Kind + status.ReadySuffix
}

func (b *BaseHandler) GetContainerName() string {
	if b.Config.ContainerName != "" {
		return b.Config.ContainerName
	}
	return "manager"
}

// GetDeploymentName returns the configured rendered Deployment name, or the
// empty string when unset (callers then fall back to the manifest-derived name).
func (b *BaseHandler) GetDeploymentName() string {
	return b.Config.DeploymentName
}

func (b *BaseHandler) GetControllerImage() string {
	return b.Config.ControllerImage
}

func (b *BaseHandler) GetInitContainerName() string {
	return b.Config.InitContainerName
}

func (b *BaseHandler) GetRelatedImages() []string {
	return b.Config.RelatedImages
}

func (b *BaseHandler) GetOperatorManifests(platform *PlatformContext) OperatorManifests {
	var result OperatorManifests

	if b.Config.ChartDir != "" && platform != nil {
		vals := make(map[string]any, len(b.Config.Values))
		maps.Copy(vals, b.Config.Values)

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
		manifestPath := b.Config.ManifestDir
		if platform != nil && platform.ManifestsBasePath != "" {
			manifestPath = filepath.Join(platform.ManifestsBasePath, b.Config.ManifestDir)
		}

		sourcePath := b.Config.SourcePath
		if platform != nil && len(b.Config.SourcePathByPlatform) > 0 {
			if sp, ok := b.Config.SourcePathByPlatform[platform.Release.Name]; ok {
				sourcePath = sp
			}
		}

		result.Manifests = []types.ManifestInfo{{
			Path:       manifestPath,
			ContextDir: b.Config.ContextDir,
			SourcePath: sourcePath,
			Namespace:  b.Config.Namespace,
		}}
	}

	return result
}

// GetModuleStatus reads the module CR by GVK+CRName and extracts status
// conditions and generation metadata for staleness detection.
//
// This default implementation performs a cluster-scoped Get (no namespace),
// which is correct for the required cluster-scoped module CRDs. Modules
// with namespace-scoped CRs would need to override this method.
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
	releaseVersion := extractPlatformReleaseVersion(u)

	return &ModuleStatus{
		Conditions:         conditions,
		ObservedGeneration: observedGen,
		Generation:         u.GetGeneration(),
		ReleaseVersion:     releaseVersion,
	}, nil
}

const platformReleaseName = "platform"

func extractPlatformReleaseVersion(u *unstructured.Unstructured) string {
	releases, found, _ := unstructured.NestedSlice(u.Object, "status", "releases")
	if !found {
		return ""
	}

	for _, item := range releases {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(entry, "name")
		if name == platformReleaseName {
			ver, _, _ := unstructured.NestedString(entry, "version")
			return ver
		}
	}

	return ""
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
			if !k8serr.IsNotFound(err) && !meta.IsNoMatchError(err) {
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

		if c.Type == "" || c.Status == "" {
			continue
		}

		conditions = append(conditions, c)
	}

	return conditions, nil
}
