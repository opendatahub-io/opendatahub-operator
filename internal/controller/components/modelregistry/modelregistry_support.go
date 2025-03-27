package modelregistry

import (
	"embed"
	"path"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
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
	ErrServiceMeshNotConfigured     = odherrors.NewStopError(status.ServiceMeshNotConfiguredMessage)
	ErrServiceMeshMemberAPINotFound = odherrors.NewStopError(ServiceMeshMemberAPINotFound)
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

	conditionTypes = []string{
		status.ConditionServiceMeshAvailable,
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
