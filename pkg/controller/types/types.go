package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/fs"
	"path"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

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

type ReconciliationRequest struct {
	*odhClient.Client

	Manager    *manager.Manager
	Conditions *conditions.Manager
	Instance   common.PlatformObject
	DSCI       *dsciv1.DSCInitialization
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
	Templates []TemplateInfo
	Resources []unstructured.Unstructured

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
	filtered := rr.Resources[:0] // Create a slice with zero length but full capacity

	for i := range rr.Resources {
		if predicate(&rr.Resources[i]) {
			continue
		}

		filtered = append(filtered, rr.Resources[i])
	}

	rr.Resources = filtered

	return nil
}

func Hash(rr *ReconciliationRequest) ([]byte, error) {
	hash := sha256.New()

	dsciGeneration := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(dsciGeneration, rr.DSCI.GetGeneration())

	instanceGeneration := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(instanceGeneration, rr.Instance.GetGeneration())

	if _, err := hash.Write([]byte(rr.Instance.GetUID())); err != nil {
		return nil, fmt.Errorf("failed to hash instance: %w", err)
	}
	if _, err := hash.Write(dsciGeneration); err != nil {
		return nil, fmt.Errorf("failed to hash dsci generation: %w", err)
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

	return hash.Sum(nil), nil
}

func HashStr(rr *ReconciliationRequest) (string, error) {
	h, err := Hash(rr)
	if err != nil {
		return "", err
	}

	return resources.EncodeToString(h), nil
}
