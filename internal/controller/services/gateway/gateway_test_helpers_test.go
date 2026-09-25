//nolint:testpackage
package gateway

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

func newGatewayTestClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	cli, err := fakeclient.New(fakeclient.WithObjects(objects...))
	if err != nil {
		t.Fatalf("failed to create fake client: %v", err)
	}
	return cli
}

func setGatewayConfigOwner(route *routev1.Route, gatewayConfig *serviceApi.GatewayConfig) {
	controller := true
	route.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         serviceApi.GroupVersion.String(),
		Kind:               serviceApi.GatewayConfigKind,
		Name:               gatewayConfig.Name,
		UID:                gatewayConfig.UID,
		Controller:         &controller,
		BlockOwnerDeletion: &controller,
	}}
}

func additionalIngress(name, hostname string, port int32, controller string) serviceApi.AdditionalIngress {
	return serviceApi.AdditionalIngress{
		Name:                  name,
		Hostname:              hostname,
		ListenerPort:          port,
		IngressControllerName: controller,
		RouteLabels:           map[string]string{"example.com/ingress": name},
	}
}
