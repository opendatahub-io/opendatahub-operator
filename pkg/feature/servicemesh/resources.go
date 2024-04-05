package servicemesh

import (
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
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
func AuthRefs(f *feature.Feature) error {
	audiences := f.Spec.Auth.Audiences
	namespace := f.Spec.AppNamespace
	audiencesList := ""
	if audiences != nil && len(*audiences) > 0 {
		audiencesList = strings.Join(*audiences, ",")
	}
	data := map[string]string{
		"AUTH_AUDIENCE":   audiencesList,
		"AUTH_PROVIDER":   namespace + "-auth-provider",
		"AUTHORINO_LABEL": "security.opendatahub.io/authorization-group=default",
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
