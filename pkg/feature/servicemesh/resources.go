package servicemesh

import (
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ConfigMaps(f *feature.Feature) error {
	if err := serviceMeshRefsConfigMap(f); err != nil {
		return err
	}

	return authRefsConfigMap(f)
}

func serviceMeshRefsConfigMap(f *feature.Feature) error {
	meshConfig := f.Spec.ControlPlane
	namespace := f.Spec.AppNamespace

	data := map[string]string{
		"CONTROL_PLANE_NAME": meshConfig.Name,
		"MESH_NAMESPACE":     meshConfig.Namespace,
	}

	_, err := cluster.CreateConfigMap(
		f.Client,
		"service-mesh-refs",
		namespace,
		data,
		feature.OwnedBy(f),
	)

	return err
}

func authRefsConfigMap(f *feature.Feature) error {
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

	_, err := cluster.CreateConfigMap(
		f.Client,
		"auth-refs",
		namespace,
		data,
		feature.OwnedBy(f),
	)

	return err
}
