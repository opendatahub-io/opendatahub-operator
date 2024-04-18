package servicemesh

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	return cluster.CreateOrUpdateConfigMap(
		f.Client,
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-mesh-refs",
				Namespace: namespace,
			},
			Data: data,
		},
		feature.OwnedBy(f),
	)
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

	return cluster.CreateOrUpdateConfigMap(
		f.Client,
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "auth-refs",
				Namespace: namespace,
			},
			Data: data,
		},
		feature.OwnedBy(f),
	)
}
