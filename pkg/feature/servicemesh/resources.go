package servicemesh

import (
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// MeshRefs stores service mesh configuration in the config map, so it can
// be easily accessed by other components which rely on this information.
func MeshRefs(f *feature.Feature) error {
	meshConfig := f.Spec.ControlPlane
	namespace := f.Spec.AppNamespace

	data := map[string]string{
		"CONTROL_PLANE_NAME": meshConfig.Name,
		"MESH_NAMESPACE":     meshConfig.Namespace,
	}

	_, err := cluster.CreateOrUpdateConfigMap(
		f.Client,
		"service-mesh-refs",
		namespace,
		data,
		feature.OwnedBy(f),
	)

	return err
}

// AuthRefs stores authorization configuration in the config map, so it can
// be easily accessed by other components which rely on this information.
func AuthRefs(audiences []string) feature.Action {
	return func(f *feature.Feature) error {
		namespace := f.Spec.AppNamespace

		audiencesList := ""
		if len(audiences) > 0 {
			audiencesList = strings.Join(audiences, ",")
		}
		data := map[string]string{
			"AUTH_AUDIENCE":   audiencesList,
			"AUTH_PROVIDER":   namespace + "-auth-provider",
			"AUTHORINO_LABEL": labels.ODH.AuthorizationGroup("default"),
		}

		_, err := cluster.CreateOrUpdateConfigMap(
			f.Client,
			"auth-refs",
			namespace,
			data,
			feature.OwnedBy(f),
		)

		return err
	}
}
