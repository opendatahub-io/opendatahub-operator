//go:build !integration

//nolint:testpackage
package gateway

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

func TestDefaultAuthReadinessPlatformBehavior(t *testing.T) {
	original := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	for _, test := range []struct {
		name                          string
		platform                      string
		oidc, certificate, deployment bool
		observed, available           int32
		want                          metav1.ConditionStatus
	}{
		{name: "OpenShift OAuth", platform: cluster.ClusterTypeOpenShift, want: metav1.ConditionTrue},
		{name: "OpenShift OIDC", platform: cluster.ClusterTypeOpenShift, oidc: true, want: metav1.ConditionTrue},
		{name: "Kubernetes no OIDC", platform: cluster.ClusterTypeKubernetes, want: metav1.ConditionTrue},
		{name: "Kubernetes certificate missing", platform: cluster.ClusterTypeKubernetes, oidc: true, want: metav1.ConditionFalse},
		{name: "Kubernetes deployment missing", platform: cluster.ClusterTypeKubernetes, oidc: true, certificate: true, want: metav1.ConditionFalse},
		{name: "Kubernetes rollout pending", platform: cluster.ClusterTypeKubernetes, oidc: true, certificate: true, deployment: true, observed: 1, want: metav1.ConditionFalse},
		{name: "Kubernetes generation stale", platform: cluster.ClusterTypeKubernetes, oidc: true, certificate: true, deployment: true, available: 2, want: metav1.ConditionFalse},
		{name: "Kubernetes ready", platform: cluster.ClusterTypeKubernetes, oidc: true, certificate: true, deployment: true, observed: 1, available: 2, want: metav1.ConditionTrue},
	} {
		t.Run(test.name, func(t *testing.T) {
			cluster.SetClusterInfo(cluster.ClusterInfo{Type: test.platform})
			{
				config := &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, Generation: 1},
					Spec: serviceApi.GatewayConfigSpec{Domain: "apps.example.com", Certificate: &infrav1.CertificateSpec{Type: infrav1.Provided}}}
				if test.oidc {
					config.Spec.OIDC = &serviceApi.OIDCConfig{}
				}
				target := defaultAuthProxyTarget("apps.example.com", config)

				objects := []client.Object{config, managedGatewayForIngress(GetDefaultGatewayName())}
				if test.certificate {
					cert := &unstructured.Unstructured{}
					cert.SetGroupVersionKind(gvk.CertManagerCertificate)
					cert.SetName(target.TLSName)
					cert.SetNamespace(GetGatewayNamespace())
					cert.SetGeneration(1)
					if err := unstructured.SetNestedSlice(cert.Object, []any{map[string]any{
						"type": "Ready", "status": "True", "observedGeneration": int64(1),
					}}, "status", "conditions"); err != nil {
						t.Fatal(err)
					}
					objects = append(objects, cert, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: target.TLSName, Namespace: GetGatewayNamespace()},
						Data: map[string][]byte{corev1.TLSCertKey: []byte("cert"), corev1.TLSPrivateKeyKey: []byte("key")}})
				}
				if test.deployment {
					objects = append(objects, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: target.ProxyName, Namespace: GetGatewayNamespace(), Generation: 1},
						Spec:   appsv1.DeploymentSpec{Replicas: new(int32(2))},
						Status: appsv1.DeploymentStatus{ObservedGeneration: int64(test.observed), AvailableReplicas: test.available}})
				}
				cli, err := fakeclient.New(fakeclient.WithObjects(objects...),
					fakeclient.WithGVKs(fakeclient.GVKMapping{GVK: gvk.CertManagerCertificate, Scope: meta.RESTScopeNamespace}))
				if err != nil {
					t.Fatal(err)
				}
				rr := &odhtypes.ReconciliationRequest{Instance: config, Client: cli, Conditions: conditionstest.NewManager(config, ReadyConditionType)}
				rr.Conditions.MarkUnknown(ReadyConditionType)
				if err := syncGatewayConfigStatus(t.Context(), rr); err != nil {
					t.Fatal(err)
				}
				got := rr.Conditions.GetCondition(ReadyConditionType)
				if got.Status != test.want {
					t.Fatalf("got %s (%s), want %s", got.Status, got.Reason, test.want)
				}
				if test.want == metav1.ConditionFalse && got.Reason != status.NotReadyReason {
					t.Fatalf("unexpected reason %s", got.Reason)
				}
			}
		})
	}
}
