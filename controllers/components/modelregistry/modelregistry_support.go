package modelregistry

import (
	"context"
	"fmt"
	"path"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/conversion"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"

	_ "embed"
)

const (
	ComponentName                   = componentsv1.ModelRegistryComponentName
	DefaultModelRegistriesNamespace = "odh-model-registries"
	DefaultModelRegistryCert        = "default-modelregistry-cert"
	BaseManifestsSourcePath         = "overlays/odh"
)

var (
	imagesMap = map[string]string{
		"IMAGES_MODELREGISTRY_OPERATOR": "RELATED_IMAGE_ODH_MODEL_REGISTRY_OPERATOR_IMAGE",
		"IMAGES_GRPC_SERVICE":           "RELATED_IMAGE_ODH_MLMD_GRPC_SERVER_IMAGE",
		"IMAGES_REST_SERVICE":           "RELATED_IMAGE_ODH_MODEL_REGISTRY_IMAGE",
	}

	extraParamsMap = map[string]string{
		"DEFAULT_CERT": DefaultModelRegistryCert,
	}
)

//go:embed resources/servicemesh-member.tmpl.yaml
var smmTemplate string

func baseManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       deploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: sourcePath,
	}
}

func extraManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       deploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: path.Join(sourcePath, "extras"),
	}
}

func createServiceMeshMember(dsci *dsciv1.DSCInitialization, namespace string) (*unstructured.Unstructured, error) {
	tmpl, err := template.New("servicemeshmember").Parse(smmTemplate)
	if err != nil {
		return nil, fmt.Errorf("error parsing servicemeshmember template: %w", err)
	}

	controlPlaneData := struct {
		Namespace    string
		ControlPlane *infrav1.ControlPlaneSpec
	}{
		Namespace:    namespace,
		ControlPlane: &dsci.Spec.ServiceMesh.ControlPlane,
	}

	builder := strings.Builder{}
	if err = tmpl.Execute(&builder, controlPlaneData); err != nil {
		return nil, fmt.Errorf("error executing servicemeshmember template: %w", err)
	}

	obj, err := conversion.StrToUnstructured(builder.String())
	if err != nil || len(obj) != 1 {
		return nil, fmt.Errorf("error converting servicemeshmember template: %w", err)
	}

	return obj[0], nil
}

func ingressSecret(ctx context.Context, cli client.Client) predicate.Funcs {
	f := func(obj client.Object) bool {
		ic, err := cluster.FindAvailableIngressController(ctx, cli)
		if err != nil {
			return false
		}
		if ic.Spec.DefaultCertificate == nil {
			return false
		}

		return obj.GetName() == ic.Spec.DefaultCertificate.Name &&
			obj.GetNamespace() == cluster.IngressNamespace
	}

	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return f(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return f(e.ObjectNew)
		},
	}
}
