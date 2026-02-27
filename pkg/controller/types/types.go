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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// Controller defines the core interface for a controller in the OpenDataHub Operator.
type Controller interface {
	// Owns returns true if the controller manages resources of the specified GroupVersionKind.
	Owns(gvk schema.GroupVersionKind) bool

	// GetClient returns a controller-runtime client used to interact with the Kubernetes API.
	GetClient() client.Client

	// GetDiscoveryClient returns a client-go discovery client used to discover API resources on the cluster.
	GetDiscoveryClient() discovery.DiscoveryInterface

	// GetDynamicClient returns a client-go dynamic client for working with unstructured resources.
	GetDynamicClient() dynamic.Interface
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
}

func (mi ManifestInfo) String() string {
	result := mi.Path

	if mi.ContextDir != "" {
		result = path.Join(result, mi.ContextDir)
	}

	if mi.SourcePath != "" {
		result = path.Join(result, mi.SourcePath)
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
	Client     client.Client
	Controller Controller
	Conditions *conditions.Manager
	Instance   common.PlatformObject
	Release    common.Release
	Manifests  []ManifestInfo

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
