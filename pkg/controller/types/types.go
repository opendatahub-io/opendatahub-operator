package types

import (
	"fmt"
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

type ReconciliationRequest struct {
	*odhClient.Client

	Manager   *manager.Manager
	Instance  components.ComponentObject
	DSC       *dscv1.DataScienceCluster
	DSCI      *dsciv1.DSCInitialization
	Release   cluster.Release
	Manifests []ManifestInfo
	Resources []unstructured.Unstructured
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
