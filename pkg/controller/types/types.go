package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/fs"
	"path"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	machineryrt "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/components"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type ResourceObject interface {
	client.Object
	components.WithStatus
}

type WithLogger interface {
	GetLogger() logr.Logger
}

type ManifestInfo struct {
	Path       string
	ContextDir string
	SourcePath string
}

func (mi *ManifestInfo) String() string {
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
}

type ReconciliationRequest struct {
	*odhClient.Client

	OwnerName string

	Manager   *manager.Manager
	Instance  components.ComponentObject
	DSC       *dscv1.DataScienceCluster
	DSCI      *dsciv1.DSCInitialization
	Release   cluster.Release
	Manifests []ManifestInfo

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

func (rr *ReconciliationRequest) AddResource(in interface{}) error {
	if obj, ok := in.(client.Object); ok {
		err := rr.normalize(obj)
		if err != nil {
			return fmt.Errorf("cannot normalize object: %w", err)
		}
	}

	u, err := machineryrt.DefaultUnstructuredConverter.ToUnstructured(in)
	if err != nil {
		return err
	}

	rr.Resources = append(rr.Resources, unstructured.Unstructured{Object: u})

	return nil
}

func (rr *ReconciliationRequest) normalize(obj client.Object) error {
	if obj.GetObjectKind().GroupVersionKind().Kind != "" {
		return nil
	}

	kinds, _, err := rr.Client.Scheme().ObjectKinds(obj)
	if err != nil {
		return fmt.Errorf("cannot get kind of resource: %w", err)
	}

	if len(kinds) != 1 {
		return fmt.Errorf("expected to find a single GVK for %v, but got %d", obj, len(kinds))
	}

	obj.GetObjectKind().SetGroupVersionKind(kinds[0])

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
