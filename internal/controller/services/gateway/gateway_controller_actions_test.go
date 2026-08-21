//go:build !integration

//nolint:testpackage
package gateway

import (
	"testing"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestXKSReconcileWithoutDomainStopsCleanly(t *testing.T) {
	g := NewWithT(t)

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	ctx := t.Context()
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			IngressMode: serviceApi.IngressModeLoadBalancer,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).NotTo(HaveOccurred())

	accessor := &gatewayConfigConditionsAccessor{}
	rr := &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   gatewayConfig,
		Conditions: conditions.NewManager(accessor, ReadyConditionType),
	}

	g.Expect(createGatewayInfrastructure(ctx, rr)).To(Succeed())
	g.Expect(createKubeAuthProxyInfrastructure(ctx, rr)).To(Succeed())
	g.Expect(createEnvoyFilter(ctx, rr)).To(Succeed())
	g.Expect(createNetworkPolicy(ctx, rr)).To(Succeed())
	g.Expect(syncGatewayConfigStatus(ctx, rr)).To(Succeed())

	ready := rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(status.NotReadyReason))
	g.Expect(ready.Message).To(Equal(status.GatewayDomainRequiredMessage))

	g.Expect(rr.Resources).To(BeEmpty(), "no gateway resources should be queued without domain")
	g.Expect(rr.Templates).To(BeEmpty(), "no templates should be queued without domain")

	gateway := &gwapiv1.Gateway{}
	err = cli.Get(ctx, types.NamespacedName{
		Name:      GetDefaultGatewayName(),
		Namespace: GetGatewayNamespace(),
	}, gateway)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "Gateway CR should not be created without domain")

	gatewayClass := &gwapiv1.GatewayClass{}
	err = cli.Get(ctx, types.NamespacedName{Name: GatewayClassName}, gatewayClass)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "GatewayClass should not be created without domain")
}
