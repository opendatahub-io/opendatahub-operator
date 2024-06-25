package servicemesh

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

// MeshRefs stores service mesh configuration in the config map, so it can
// be easily accessed by other components which rely on this information.
func MeshRefs(ctx context.Context, f *feature.Feature) error {
	meshConfig := f.Spec.ControlPlane
	namespace := f.Spec.AppNamespace

	data := map[string]string{
		"CONTROL_PLANE_NAME": meshConfig.Name,
		"MESH_NAMESPACE":     meshConfig.Namespace,
	}

	return cluster.CreateOrUpdateConfigMap(
		ctx,
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
func AuthRefs(ctx context.Context, f *feature.Feature) error {
	audiences := f.Spec.Auth.Audiences
	appNamespace := f.Spec.AppNamespace
	authNamespace := f.Spec.Auth.Namespace
	if len(authNamespace) == 0 {
		authNamespace = appNamespace + "-auth-provider"
	}
	audiencesList := ""
	if audiences != nil && len(*audiences) > 0 {
		audiencesList = strings.Join(*audiences, ",")
	}
	data := map[string]string{
		"AUTH_AUDIENCE":   audiencesList,
		"AUTH_PROVIDER":   appNamespace + "-auth-provider",
		"AUTH_NAMESPACE":  authNamespace,
		"AUTHORINO_LABEL": "security.opendatahub.io/authorization-group=default",
	}

	return cluster.CreateOrUpdateConfigMap(
		ctx,
		f.Client,
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "auth-refs",
				Namespace: appNamespace,
			},
			Data: data,
		},
		feature.OwnedBy(f),
	)
}
