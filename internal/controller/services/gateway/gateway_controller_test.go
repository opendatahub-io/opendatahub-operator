//go:build !integration

//nolint:testpackage
package gateway

import (
	"context"
	"errors"
	"slices"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

// TestGatewayDeployFailurePropagatesAndReadinessRecovers pins the standard deploy
// action contract the gateway relies on: an apply failure must propagate as an
// error so the reconciler records it on ProvisioningSucceeded (and hence the
// top-level Ready condition), and a recovered apply followed by
// syncGatewayConfigStatus must return GatewayConfigReady to True.
func TestGatewayDeployFailurePropagatesAndReadinessRecovers(t *testing.T) {
	g := NewWithT(t)
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})

	blocked := true
	proxyPolicyError := errors.New("networkpolicies.networking.k8s.io \"kube-auth-proxy\" is forbidden")
	gatewayConfig := &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{
		Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"),
	}}
	hostname := gwapiv1.Hostname("gateway.example.com")
	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: GetDefaultGatewayName(), Namespace: GetGatewayNamespace()},
		Spec:       gwapiv1.GatewaySpec{Listeners: []gwapiv1.Listener{{Hostname: &hostname}}},
		Status: gwapiv1.GatewayStatus{Conditions: []metav1.Condition{{
			Type: string(gwapiv1.GatewayConditionAccepted), Status: metav1.ConditionTrue,
		}}},
	}
	cli, err := fakeclient.New(
		fakeclient.WithObjects(gatewayConfig, gateway),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if blocked && obj.GetObjectKind().GroupVersionKind() == gvk.NetworkPolicy {
					return proxyPolicyError
				}
				return cl.Create(ctx, obj, opts...)
			},
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   gatewayConfig,
		Conditions: conditionstest.NewManager(gatewayConfig, ReadyConditionType),
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", gvk.NetworkPolicy).Return(true)
		}),
	}
	g.Expect(rr.AddResources(&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
		Name: KubeAuthProxyName, Namespace: GetGatewayNamespace(),
	}})).To(Succeed())
	deployAction := deploy.NewAction(deploy.WithMode(deploy.ModePatch))
	rr.Conditions.MarkTrue(ReadyConditionType, conditions.WithReason(status.ReadyReason))

	// The apply failure must surface as an error; the reconciler marks
	// ProvisioningSucceeded (and the top-level Ready condition) False with it.
	err = deployAction(t.Context(), rr)
	g.Expect(err).To(MatchError(ContainSubstring(proxyPolicyError.Error())))

	blocked = false
	rr.Conditions.MarkUnknown(ReadyConditionType) // The proxy action resets readiness on retry.
	g.Expect(deployAction(t.Context(), rr)).To(Succeed())
	g.Expect(syncGatewayConfigStatus(t.Context(), rr)).To(Succeed())
	ready := rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(ready.Reason).To(Equal(status.ReadyReason))
	g.Expect(cli.Get(t.Context(), types.NamespacedName{
		Name: KubeAuthProxyName, Namespace: GetGatewayNamespace(),
	}, &networkingv1.NetworkPolicy{})).To(Succeed())
}

// TestGatewayCRDWatchPredicate pins the set of CRDs whose installation or removal re-triggers a
// GatewayConfig reconcile. The cert-manager Certificate CRD matters because XKS certificate
// handling is blocked until cert-manager is installed; without this event a cluster that installs
// cert-manager after the operator started would wait for an unrelated reconcile.
func TestGatewayCRDWatchPredicate(t *testing.T) {
	t.Parallel()

	pred := gatewayCRDWatchPredicate()

	for _, tc := range []struct {
		name      string
		crdName   string
		wantMatch bool
	}{
		{name: "cert-manager Certificate CRD", crdName: gvk.CertManagerCertificateCRDName, wantMatch: true},
		{name: "Dashboard CRD", crdName: gvk.DashboardComponentCRDName, wantMatch: true},
		{name: "unrelated CRD", crdName: "widgets.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			crd := &extv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: tc.crdName}}

			g.Expect(pred.Create(event.CreateEvent{Object: crd})).To(Equal(tc.wantMatch))
			g.Expect(pred.Update(event.UpdateEvent{ObjectOld: crd, ObjectNew: crd})).To(Equal(tc.wantMatch))
			g.Expect(pred.Delete(event.DeleteEvent{Object: crd})).To(Equal(tc.wantMatch))
		})
	}
}

func TestGatewayAuthenticationWatchPredicate(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	newAuth := func(name, authType string) *unstructured.Unstructured {
		auth := &unstructured.Unstructured{}
		auth.SetName(name)
		g.Expect(unstructured.SetNestedField(auth.Object, authType, "spec", "type")).To(Succeed())
		return auth
	}
	newTypedAuth := func(name string, authType configv1.AuthenticationType) *configv1.Authentication {
		return &configv1.Authentication{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       configv1.AuthenticationSpec{Type: authType},
		}
	}

	pred := gatewayAuthenticationWatchPredicate()
	oldAuth := newAuth(cluster.ClusterAuthenticationObj, "IntegratedOAuth")
	g.Expect(pred.Create(event.CreateEvent{Object: oldAuth})).To(BeTrue())
	g.Expect(pred.Delete(event.DeleteEvent{Object: oldAuth})).To(BeTrue())
	g.Expect(pred.Update(event.UpdateEvent{ObjectOld: oldAuth, ObjectNew: newAuth(cluster.ClusterAuthenticationObj, "None")})).To(BeTrue())
	g.Expect(pred.Update(event.UpdateEvent{ObjectOld: oldAuth, ObjectNew: newAuth(cluster.ClusterAuthenticationObj, "IntegratedOAuth")})).To(BeFalse())
	g.Expect(pred.Update(event.UpdateEvent{ObjectOld: oldAuth, ObjectNew: newAuth("other", "None")})).To(BeFalse())
	g.Expect(pred.Create(event.CreateEvent{Object: newAuth("other", "None")})).To(BeFalse())

	// The watch is registered for typed &configv1.Authentication, so typed events must
	// be filtered on spec.type exactly like the unstructured (Dynamic) form.
	typedAuth := newTypedAuth(cluster.ClusterAuthenticationObj, configv1.AuthenticationTypeIntegratedOAuth)

	// A typed update that changes only metadata must not trigger reconciliation.
	typedUnchanged := typedAuth.DeepCopy()
	typedUnchanged.ResourceVersion = "2"
	g.Expect(pred.Update(event.UpdateEvent{ObjectOld: typedAuth, ObjectNew: typedUnchanged})).To(BeFalse())

	// A change to the authentication type must trigger reconciliation.
	typedChanged := typedAuth.DeepCopy()
	typedChanged.Spec.Type = configv1.AuthenticationTypeNone
	g.Expect(pred.Update(event.UpdateEvent{ObjectOld: typedAuth, ObjectNew: typedChanged})).To(BeTrue())

	// Create and delete work for the typed object too.
	g.Expect(pred.Create(event.CreateEvent{Object: typedAuth})).To(BeTrue())
	g.Expect(pred.Delete(event.DeleteEvent{Object: typedAuth})).To(BeTrue())

	// A malformed unstructured type still triggers the fail-safe path.
	malformedAuth := &unstructured.Unstructured{}
	malformedAuth.SetName(cluster.ClusterAuthenticationObj)
	g.Expect(unstructured.SetNestedField(malformedAuth.Object, int64(1), "spec", "type")).To(Succeed())
	g.Expect(pred.Update(event.UpdateEvent{ObjectOld: oldAuth, ObjectNew: malformedAuth})).To(BeTrue())
}

func TestCleanupKubeAuthProxyResources(t *testing.T) {
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	// On Kubernetes clusters the auth mode comes from the GatewayConfig OIDC
	// spec, so the test controls whether the proxy is desired without setting
	// up an Authentication CR.
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

	gatewayUID := types.UID("gateway-config-uid")
	partOf := map[string]string{labels.PlatformPartOf: labels.NormalizePartOfValue(serviceApi.GatewayConfigKind)}
	gatewayNamespace := GetGatewayNamespace()

	newManagedDeployment := func() *appsv1.Deployment {
		return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxyName,
			Namespace: gatewayNamespace,
			Labels:    partOf,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: serviceApi.GroupVersion.String(), Kind: serviceApi.GatewayConfigKind, UID: gatewayUID,
			}},
		}}
	}
	newLegacyPolicy := func() *networkingv1.NetworkPolicy {
		return &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
			Name: KubeAuthProxyName, Namespace: gatewayNamespace, Labels: partOf,
		}}
	}
	newUnmanagedPolicy := func() *networkingv1.NetworkPolicy {
		return &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
			Name:        KubeAuthProxyName,
			Namespace:   gatewayNamespace,
			Labels:      partOf,
			Annotations: map[string]string{"opendatahub.io/managed": "false"},
		}}
	}
	newForeignDeployment := func() *appsv1.Deployment {
		return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name: KubeAuthProxyName, Namespace: gatewayNamespace, Labels: map[string]string{"app": "foreign"},
		}}
	}

	for _, tc := range []struct {
		name     string
		withOIDC bool
		objects  func() []client.Object
		removed  []string
	}{
		{
			name:     "proxy not desired removes managed and legacy resources",
			withOIDC: false,
			objects: func() []client.Object {
				return []client.Object{newManagedDeployment(), newLegacyPolicy()}
			},
			removed: []string{"Deployment", "NetworkPolicy"},
		},
		{
			name:     "explicitly unmanaged policy is kept",
			withOIDC: false,
			objects: func() []client.Object {
				return []client.Object{newUnmanagedPolicy()}
			},
		},
		{
			name:     "foreign deployment sharing the name is kept",
			withOIDC: false,
			objects: func() []client.Object {
				return []client.Object{newForeignDeployment()}
			},
		},
		{
			name:     "proxy desired keeps everything",
			withOIDC: true,
			objects: func() []client.Object {
				return []client.Object{newManagedDeployment(), newLegacyPolicy()}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, UID: gatewayUID},
			}
			gatewayConfig.Spec.AdditionalIngresses = serviceApi.AdditionalIngresses{
				additionalIngress("alpha", "alpha.example.com", "alpha"),
			}
			if tc.withOIDC {
				gatewayConfig.Spec.OIDC = &serviceApi.OIDCConfig{IssuerURL: "https://issuer.example.com"}
			}

			objects := tc.objects()
			for _, obj := range tc.objects() {
				obj.SetName("kube-auth-proxy-alpha")
				objects = append(objects, obj)
			}
			cli, err := fakeclient.New(fakeclient.WithObjects(objects...))
			g.Expect(err).NotTo(HaveOccurred())

			rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}
			g.Expect(cleanupKubeAuthProxyResources(t.Context(), rr)).To(Succeed())

			assert := func(obj client.Object, kind string) {
				err := cli.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)
				if slices.Contains(tc.removed, kind) {
					g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "the proxy %s should be removed", kind)
				} else {
					g.Expect(err).NotTo(HaveOccurred(), "the proxy %s should be kept", kind)
				}
			}

			for _, obj := range objects {
				switch obj.(type) {
				case *appsv1.Deployment:
					assert(obj, gvk.Deployment.Kind)
				case *networkingv1.NetworkPolicy:
					assert(obj, gvk.NetworkPolicy.Kind)
				}
			}
		})
	}
}

func TestGatewayDeploymentWatchPredicate(t *testing.T) {
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

	newDeployment := func(name, namespace string, generation, available, observed int64) *unstructured.Unstructured {
		deployment := &unstructured.Unstructured{}
		deployment.SetGroupVersionKind(gvk.Deployment)
		deployment.SetName(name)
		deployment.SetNamespace(namespace)
		deployment.SetGeneration(generation)
		if err := unstructured.SetNestedField(deployment.Object, available, "status", "availableReplicas"); err != nil {
			t.Fatal(err)
		}
		if err := unstructured.SetNestedField(deployment.Object, observed, "status", "observedGeneration"); err != nil {
			t.Fatal(err)
		}
		return deployment
	}

	for _, tc := range []struct {
		name        string
		deployName  string
		namespace   string
		generation  int64
		available   int64
		observed    int64
		wantTrigger bool
	}{
		{name: "proxy becomes available", deployName: KubeAuthProxyName, namespace: GetGatewayNamespace(), generation: 1, available: 2, observed: 1, wantTrigger: true},
		{name: "controller observes the generation", deployName: KubeAuthProxyName, namespace: GetGatewayNamespace(), generation: 1, available: 0, observed: 2, wantTrigger: true},
		{name: "status unchanged", deployName: KubeAuthProxyName, namespace: GetGatewayNamespace(), generation: 1, available: 0, observed: 1},
		{name: "unrelated deployment status", deployName: "other", namespace: GetGatewayNamespace(), generation: 1, available: 2, observed: 1},
		{name: "other namespace status", deployName: KubeAuthProxyName, namespace: "other", generation: 1, available: 2, observed: 1},
		{name: "spec change still triggers", deployName: KubeAuthProxyName, namespace: GetGatewayNamespace(), generation: 2, available: 0, observed: 1, wantTrigger: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldDeployment := newDeployment(tc.deployName, tc.namespace, 1, 0, 1)
			updatedDeployment := newDeployment(tc.deployName, tc.namespace, tc.generation, tc.available, tc.observed)
			triggered := gatewayDeploymentWatchPredicate().Update(event.UpdateEvent{
				ObjectOld: oldDeployment,
				ObjectNew: updatedDeployment,
			})
			if triggered != tc.wantTrigger {
				t.Errorf("watch triggered = %t, want %t", triggered, tc.wantTrigger)
			}
		})
	}
}

func TestGatewayCertManagerPrecondition(t *testing.T) {
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

	for _, tc := range []struct {
		name        string
		certificate *infrav1.CertificateSpec
		oidc        *serviceApi.OIDCConfig
		wantStop    bool
	}{
		{name: "provided TLS without auth proxy", certificate: &infrav1.CertificateSpec{Type: infrav1.Provided}},
		{name: "provided TLS with auth proxy", certificate: &infrav1.CertificateSpec{Type: infrav1.Provided}, oidc: &serviceApi.OIDCConfig{}, wantStop: true},
		{name: "self-signed TLS", certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned}, wantStop: true},
		{name: "default TLS", wantStop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Spec: serviceApi.GatewayConfigSpec{
					Domain:      "apps.example.com",
					IngressMode: serviceApi.IngressModeLoadBalancer,
					Certificate: tc.certificate,
					OIDC:        tc.oidc,
				},
			}
			cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
			g.Expect(err).NotTo(HaveOccurred())
			rr := &odhtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   gatewayConfig,
				Conditions: conditionstest.NewManager(gatewayConfig, ReadyConditionType),
			}

			stop := precondition.RunAll(t.Context(), rr, []precondition.PreCondition{gatewayCertManagerPrecondition()})
			g.Expect(stop).To(Equal(tc.wantStop))
			dependency := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(dependency).NotTo(BeNil())
			if tc.wantStop {
				g.Expect(dependency.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(dependency.Message).To(Equal("cert-manager Certificate CRD is required for XKS certificate issuance"))
				return
			}

			g.Expect(dependency.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(createGatewayInfrastructure(t.Context(), rr)).To(Succeed())
			g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
			g.Expect(rr.Resources).To(HaveLen(3), "Gateway infrastructure plus the default auth filter")
			g.Expect(rr.Resources[0].GroupVersionKind()).To(Equal(gvk.GatewayClass))
			g.Expect(rr.Resources[1].GroupVersionKind()).To(Equal(gvk.KubernetesGateway))
			g.Expect(rr.Resources[1].GetName()).To(Equal(GetDefaultGatewayName()))
			g.Expect(rr.Resources[1].GetNamespace()).To(Equal(GetGatewayNamespace()))
			g.Expect(rr.Templates).To(BeEmpty(), "AuthModeNone should not queue auth proxy resources")
		})
	}
}

func TestGatewayCertManagerPreconditionPassesWhenCRDIsPresent(t *testing.T) {
	g := NewWithT(t)
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned},
		},
	}
	certManagerCRD := &extv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: gvk.CertManagerCertificateCRDName},
		Status: extv1.CustomResourceDefinitionStatus{
			Conditions: []extv1.CustomResourceDefinitionCondition{{
				Type:   extv1.Established,
				Status: extv1.ConditionTrue,
			}},
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, certManagerCRD))
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   gatewayConfig,
		Conditions: conditionstest.NewManager(gatewayConfig, ReadyConditionType),
	}

	g.Expect(precondition.RunAll(t.Context(), rr, []precondition.PreCondition{gatewayCertManagerPrecondition()})).To(BeFalse())
	g.Expect(rr.Conditions.GetCondition(status.ConditionDependenciesAvailable).Status).To(Equal(metav1.ConditionTrue))
}

func TestGatewayCertManagerPreconditionSkippedOnOpenShift(t *testing.T) {
	g := NewWithT(t)
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned},
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   gatewayConfig,
		Conditions: conditionstest.NewManager(gatewayConfig, ReadyConditionType),
	}

	g.Expect(precondition.RunAll(t.Context(), rr, []precondition.PreCondition{gatewayCertManagerPrecondition()})).To(BeFalse())
	g.Expect(rr.Conditions.GetCondition(status.ConditionDependenciesAvailable).Status).To(Equal(metav1.ConditionTrue))
}

func TestGatewayCertManagerPreconditionClearsDependencyAfterSwitchToProvided(t *testing.T) {
	g := NewWithT(t)
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned},
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   gatewayConfig,
		Conditions: conditionstest.NewManager(gatewayConfig, ReadyConditionType),
	}
	pc := gatewayCertManagerPrecondition()

	g.Expect(precondition.RunAll(t.Context(), rr, []precondition.PreCondition{pc})).To(BeTrue())
	g.Expect(rr.Conditions.GetCondition(status.ConditionDependenciesAvailable).Status).To(Equal(metav1.ConditionFalse))

	gatewayConfig.Spec.Certificate.Type = infrav1.Provided
	rr.Conditions.Reset()
	g.Expect(precondition.RunAll(t.Context(), rr, []precondition.PreCondition{pc})).To(BeFalse())
	g.Expect(rr.Conditions.GetCondition(status.ConditionDependenciesAvailable).Status).To(Equal(metav1.ConditionTrue))
}
