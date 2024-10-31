package types

import (
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

func (rr *ReconciliationRequest) AddResource(obj interface{}) error {
	u, err := machineryrt.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}

	rr.Resources = append(rr.Resources, unstructured.Unstructured{Object: u})

	return nil
}
