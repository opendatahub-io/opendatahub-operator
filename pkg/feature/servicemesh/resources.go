package servicemesh

import (
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

	return nil
}
