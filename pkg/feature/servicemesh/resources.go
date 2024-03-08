package servicemesh

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ConfigMaps(feature *feature.Feature) error {
	meshConfig := feature.Spec.ControlPlane
	if err := feature.CreateConfigMap("service-mesh-refs",
		map[string]string{
			"CONTROL_PLANE_NAME": meshConfig.Name,
			"MESH_NAMESPACE":     meshConfig.Namespace,
		}); err != nil {
		return errors.WithStack(err)
	}

	audiences := feature.Spec.Auth.Audiences
	audiencesList := ""
	if audiences != nil && len(*audiences) > 0 {
		audiencesList = strings.Join(*audiences, ",")
	}
	if err := feature.CreateConfigMap("auth-refs",
		map[string]string{
			"AUTH_AUDIENCE":   audiencesList,
			"AUTH_PROVIDER":   feature.Spec.AppNamespace + "-auth-provider",
			"AUTHORINO_LABEL": "security.opendatahub.io/authorization-group=default",
		}); err != nil {
		return errors.WithStack(err)
	}
	return nil
}
