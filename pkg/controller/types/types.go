package types

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"

	"github.com/go-logr/logr"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// ModuleEnvInjection holds aggregated environment variable injection data
// for all enabled modules. Set by provisionModules and consumed by the
// injectModuleEnv action to inject RELATED_IMAGE_* and APPLICATIONS_NAMESPACE
// env vars into module operator Deployments.
type ModuleEnvInjection struct {
	// PerModuleImages maps each module's related images to its chart/manifest
	// resources. Each entry's images are only injected into Deployments
	// rendered from that module's operator manifests.
	PerModuleImages []ModuleImages
	// ApplicationsNamespace is the platform's shared application namespace.
	ApplicationsNamespace string
}

// ModuleImages associates a module's related images with a deployment name
// pattern so injection can be scoped to that module's operator Deployment.
type ModuleImages struct {
	// DeploymentName is the expected name of the module's operator Deployment.
	// Typically the Helm release name, or the module handler name for Kustomize modules.
	DeploymentName string
	// ContainerName is the target container within the Deployment.
	// Defaults to "manager" (the kubebuilder convention).
	ContainerName string
	// ControllerImage is the RELATED_IMAGE_* env var name whose value
	// replaces the target container's image field. Empty means no override.
	ControllerImage string
	// InitContainerName is the name of an init container whose image field
	// should also be overridden with the ControllerImage value.
	InitContainerName string
	// Images is the list of RELATED_IMAGE_* env var names for this module.
	Images []string
}

// Controller defines the core interface for a controller in the OpenDataHub Operator.
type Controller interface {
	// Owns returns true if the controller manages resources of the specified GroupVersionKind.
	// This includes both static ownership declared via .Owns() in the builder and dynamic
	// ownership tracked via AddDynamicOwnedType.
	Owns(gvk schema.GroupVersionKind) bool

	// AddDynamicOwnedType registers a GVK as dynamically owned by this controller.
	// This is called by the dynamic ownership action after registering a watch for a resource type.
	// Once added, Owns() will return true for this GVK.
	AddDynamicOwnedType(gvk schema.GroupVersionKind)

	// GetClient returns a controller-runtime client used to interact with the Kubernetes API.
	GetClient() client.Client

	// GetDiscoveryClient returns a client-go discovery client used to discover API resources on the cluster.
	GetDiscoveryClient() discovery.DiscoveryInterface

	// GetDynamicClient returns a client-go dynamic client for working with unstructured resources.
	GetDynamicClient() dynamic.Interface

	// IsDynamicOwnershipEnabled returns true if the controller has dynamic ownership enabled.
	IsDynamicOwnershipEnabled() bool

	// IsExcludedFromDynamicOwnership returns true if the GVK should not have owner references set.
	// This is used to share exclusion configuration between deploy and dynamicownership actions.
	IsExcludedFromDynamicOwnership(gvk schema.GroupVersionKind) bool
}

type ResourceObject interface {
	client.Object
	common.WithStatus
}

type WithLogger interface {
	GetLogger() logr.Logger
}

type ManifestInfo struct {
	Path       string
	ContextDir string
	SourcePath string

	// Namespace overrides the default ApplicationsNamespace for Kustomize
	// rendering. When empty, the render action uses ApplicationsNamespace.
	// Set this for modules that deploy into a dedicated namespace.
	Namespace string
}

func (mi ManifestInfo) String() string {
	result := mi.Path

	if mi.ContextDir != "" {
		result = path.Join(result, mi.ContextDir)
	}

	if mi.SourcePath != "" {
		result = path.Join(result, mi.SourcePath)
	}

	if mi.Namespace != "" {
		result += "@ns=" + mi.Namespace
	}

	return result
}

type TemplateInfo struct {
	FS   fs.FS
	Path string

	Labels      map[string]string
	Annotations map[string]string
}

// HookFn is the signature for pre/post apply hooks.
type HookFn func(ctx context.Context, rr *ReconciliationRequest) error

// OperatorCR identifies the custom resource created by the operator that
// this chart deploys. Used by the two-phase cleanup: when a dependency is
// set to Unmanaged, the CR is filtered from deploy so GC can delete it
// while operator resources are kept alive.
type OperatorCR struct {
	GVK       schema.GroupVersionKind
	Name      string
	Namespace string
}

// HelmChartInfo describes a Helm chart to render.
type HelmChartInfo struct {
	helm.Source

	// PreApply hooks run before this chart's resources are deployed.
	// Hooks are executed in order; execution stops on the first error.
	PreApply []HookFn
	// PostApply hooks run after this chart's resources are deployed.
	// Hooks are executed in order; execution stops on the first error.
	PostApply []HookFn
}

type ReconciliationRequest struct {
	Client            client.Client
	Controller        Controller
	Conditions        *conditions.Manager
	Instance          common.PlatformObject
	Release           common.Release
	ManifestsBasePath string
	ChartsBasePath    string
	Manifests         []ManifestInfo

	//
	// TODO: unify templates and resources.
	//
	// Unfortunately, the kustomize APIs do not yet support a FileSystem that is
	// backed by golang's fs.Fs so it is not simple to have a single abstraction
	// for both the manifests types.
	//
	// it would be nice to have a structure like:
	//
	// struct {
	//   FS  fs.FS
	//   URI net.URL
	// }
	//
	// where the URI could be something like:
	// - kustomize:///path/to/overlay
	// - template:///path/to/resource.tmpl.yaml
	//
	// and use the scheme as discriminator for the rendering engine
	//
	Templates  []TemplateInfo
	HelmCharts []HelmChartInfo
	Resources  []unstructured.Unstructured

	// TODO: this has been added to reduce GC work and only run when
	//       resources have been generated. It should be removed and
	//       replaced with a better way of describing resources and
	//       their origin
	Generated bool

	// ModuleEnvInjection holds aggregated env var injection data for module
	// operator Deployments. Set by provisionModules, consumed by
	// injectModuleEnv. Nil when no modules are enabled.
	ModuleEnvInjection *ModuleEnvInjection

	// DSCI is the DSCInitialization instance fetched by provisionModules.
	// Stored here so updateModuleStatus can build a PlatformContext without
	// a duplicate API call.
	DSCI *dsciv2.DSCInitialization
}

// AddResources adds one or more resources to the ReconciliationRequest's Resources slice.
// Each provided client.Object is normalized by ensuring it has the appropriate GVK and is
// converted into an unstructured.Unstructured format before being appended to the list.
func (rr *ReconciliationRequest) AddResources(values ...client.Object) error {
	for i := range values {
		if values[i] == nil {
			continue
		}

		err := resources.EnsureGroupVersionKind(rr.Client.Scheme(), values[i])
		if err != nil {
			return fmt.Errorf("cannot normalize object: %w", err)
		}

		u, err := resources.ToUnstructured(values[i])
		if err != nil {
			return fmt.Errorf("cannot convert object to Unstructured: %w", err)
		}

		rr.Resources = append(rr.Resources, *u)
	}

	return nil
}

// ForEachResource iterates over each resource in the ReconciliationRequest's Resources slice,
// invoking the provided function `fn` for each resource. The function `fn` takes a pointer to
// an unstructured.Unstructured object and returns a boolean and an error.
//
// The iteration stops early if:
//   - `fn` returns an error.
//   - `fn` returns `true` as the first return value (`stop`).
func (rr *ReconciliationRequest) ForEachResource(fn func(*unstructured.Unstructured) (bool, error)) error {
	for i := range rr.Resources {
		stop, err := fn(&rr.Resources[i])
		if err != nil {
			return fmt.Errorf("cannot process resource %s: %w", rr.Resources[i].GroupVersionKind(), err)
		}
		if stop {
			break
		}
	}

	return nil
}

// RemoveResources removes resources from the ReconciliationRequest's Resources slice
// based on a provided predicate function. The predicate determines whether a resource
// should be removed.
//
// Parameters:
//   - predicate: A function that takes a pointer to an unstructured.Unstructured object
//     and returns a boolean indicating whether the resource should be removed.
func (rr *ReconciliationRequest) RemoveResources(predicate func(*unstructured.Unstructured) bool) error {
	// Use in-place filtering to avoid allocations
	writeIndex := 0
	for readIndex := range rr.Resources {
		if !predicate(&rr.Resources[readIndex]) {
			if writeIndex != readIndex {
				rr.Resources[writeIndex] = rr.Resources[readIndex]
			}
			writeIndex++
		}
	}

	// Clear references to help GC
	for i := writeIndex; i < len(rr.Resources); i++ {
		rr.Resources[i] = unstructured.Unstructured{}
	}

	rr.Resources = rr.Resources[:writeIndex]
	return nil
}

func Hash(rr *ReconciliationRequest) ([]byte, error) {
	hash := sha256.New()

	instanceGeneration := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(instanceGeneration, rr.Instance.GetGeneration())

	if _, err := hash.Write([]byte(rr.Instance.GetUID())); err != nil {
		return nil, fmt.Errorf("failed to hash instance: %w", err)
	}
	if _, err := hash.Write(instanceGeneration); err != nil {
		return nil, fmt.Errorf("failed to hash instance generation: %w", err)
	}
	if _, err := hash.Write([]byte(rr.Release.Name)); err != nil {
		return nil, fmt.Errorf("failed to hash release: %w", err)
	}
	if _, err := hash.Write([]byte(rr.Release.Version.String())); err != nil {
		return nil, fmt.Errorf("failed to hash release: %w", err)
	}

	for i := range rr.Manifests {
		if _, err := hash.Write([]byte(rr.Manifests[i].String())); err != nil {
			return nil, fmt.Errorf("failed to hash manifest: %w", err)
		}
	}
	for i := range rr.Templates {
		if _, err := hash.Write([]byte(rr.Templates[i].Path)); err != nil {
			return nil, fmt.Errorf("failed to hash template: %w", err)
		}
	}
	for i := range rr.HelmCharts {
		if _, err := hash.Write([]byte(rr.HelmCharts[i].Chart)); err != nil {
			return nil, fmt.Errorf("failed to hash helm chart: %w", err)
		}
		if _, err := hash.Write([]byte(rr.HelmCharts[i].ReleaseName)); err != nil {
			return nil, fmt.Errorf("failed to hash helm chart release name: %w", err)
		}
		if rr.HelmCharts[i].Values != nil {
			// json marshal the values to ensure the order is deterministic
			values, err := rr.HelmCharts[i].Values(context.TODO())
			if err != nil {
				return nil, fmt.Errorf("failed to get helm chart values: %w", err)
			}
			b, err := json.Marshal(values)
			if err != nil {
				return nil, fmt.Errorf("failed to hash helm chart values: %w", err)
			}
			if _, err := hash.Write(b); err != nil {
				return nil, fmt.Errorf("failed to hash helm chart values: %w", err)
			}
		}
	}

	return hash.Sum(nil), nil
}

func HashStr(rr *ReconciliationRequest) (string, error) {
	h, err := Hash(rr)
	if err != nil {
		return "", err
	}

	return resources.EncodeToString(h), nil
}
