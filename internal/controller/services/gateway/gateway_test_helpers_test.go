//nolint:testpackage
package gateway

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

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

func setGatewayConfigOwner(obj client.Object, gatewayConfig *serviceApi.GatewayConfig) {
	obj.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(gatewayConfig, serviceApi.GroupVersion.WithKind(serviceApi.GatewayConfigKind)),
	})
}

func additionalIngress(name, hostname, controller string) serviceApi.AdditionalIngress {
	return serviceApi.AdditionalIngress{
		Name:                  name,
		Hostname:              hostname,
		IngressControllerName: controller,
		RouteLabels:           map[string]string{"example.com/ingress": name},
	}
}

func managedGatewayForIngress(ingressName string) *gwapiv1.Gateway {
	return &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       ingressName,
			Namespace:  GetGatewayNamespace(),
			UID:        types.UID("gateway-uid-" + ingressName),
			Generation: 3,
		},
		Spec: gwapiv1.GatewaySpec{Listeners: []gwapiv1.Listener{{
			Name: gwapiv1.SectionName(DefaultGatewayListenerName), Port: gwapiv1.PortNumber(StandardHTTPSPort),
		}}},
		Status: gwapiv1.GatewayStatus{Conditions: []metav1.Condition{
			{Type: string(gwapiv1.GatewayConditionAccepted), Status: metav1.ConditionTrue, ObservedGeneration: 3},
		}},
	}
}
