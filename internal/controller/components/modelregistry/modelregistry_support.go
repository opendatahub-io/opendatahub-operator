package modelregistry

import (
	"context"
	"embed"
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.ModelRegistryComponentName

	ReadyConditionType = componentApi.ModelRegistryKind + status.ReadySuffix

	DefaultModelRegistriesNamespace = "rhoai-model-registries"
	DefaultModelRegistryCert        = "default-modelregistry-cert"
	BaseManifestsSourcePath         = "overlays/odh"
	ServiceMeshMemberTemplate       = "resources/servicemesh-member.tmpl.yaml"
	ServiceMeshMemberCRD            = "servicemeshmembers.maistra.io"
	ServiceMeshMemberAPINotFound    = "ServiceMeshMember API not found"
	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName = "model-registry-operator"
)

var (
	ErrServiceMeshMemberAPINotFound = odherrors.NewStopError(ServiceMeshMemberAPINotFound)
)

var (
	imagesMap = map[string]string{
		"IMAGES_MODELREGISTRY_OPERATOR": "RELATED_IMAGE_ODH_MODEL_REGISTRY_OPERATOR_IMAGE",
		"IMAGES_GRPC_SERVICE":           "RELATED_IMAGE_ODH_MLMD_GRPC_SERVER_IMAGE",
		"IMAGES_REST_SERVICE":           "RELATED_IMAGE_ODH_MODEL_REGISTRY_IMAGE",
		"IMAGES_OAUTH_PROXY":            "RELATED_IMAGE_OSE_OAUTH_PROXY_IMAGE",
	}

	extraParamsMap = map[string]string{
		"DEFAULT_CERT": DefaultModelRegistryCert,
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

//go:embed resources
var resourcesFS embed.FS

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

func ifGVKWatched(kvg schema.GroupVersionKind) func(context.Context, *odhtypes.ReconciliationRequest) bool {
	return func(ctx context.Context, rr *odhtypes.ReconciliationRequest) bool {
		if rr.DSCI.Spec.ServiceMesh != nil && rr.DSCI.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
			hasCRD, err := cluster.HasCRD(ctx, rr.Client, kvg)
			if err != nil {
				ctrl.Log.Error(err, "error checking if CRD installed", "GVK", kvg)
				return false
			}
			return hasCRD
		}
		return false
	}
}
