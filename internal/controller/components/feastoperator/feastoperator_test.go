//nolint:testpackage,dupl
package feastoperator

import (
	"context"
	"encoding/json"
	"testing"

	gt "github.com/onsi/gomega/types"
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.FeastOperatorComponentName))
}

func TestNewCRObject(t *testing.T) {
	handler := &componentHandler{}

	g := NewWithT(t)
	dsc := createDSCWithFeastOperator(operatorv1.Managed)

	cl, err := fakeclient.New()
	g.Expect(err).To(Succeed())

	cr, err := handler.NewCRObject(context.Background(), cl, dsc)
	g.Expect(err).To(Succeed())
	g.Expect(cr).ShouldNot(BeNil())
	g.Expect(cr).Should(BeAssignableToTypeOf(&componentApi.FeastOperator{}))

	g.Expect(cr).Should(WithTransform(json.Marshal, And(
		jq.Match(`.metadata.name == "%s"`, componentApi.FeastOperatorInstanceName),
		jq.Match(`.kind == "%s"`, componentApi.FeastOperatorKind),
		jq.Match(`.apiVersion == "%s"`, componentApi.GroupVersion),
		jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func feastTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s, err := testscheme.New()
	g := NewWithT(t)
	g.Expect(err).To(Succeed())
	g.Expect(configv1.AddToScheme(s)).To(Succeed())
	return s
}

func TestNewCRObject_WithOIDC(t *testing.T) {
	handler := &componentHandler{}
	g := NewWithT(t)
	dsc := createDSCWithFeastOperator(operatorv1.Managed)

	s := feastTestScheme(t)
	auth := createClusterAuth(configv1.AuthenticationType("OIDC"))
	gc := &serviceApi.GatewayConfig{}
	gc.SetName(serviceApi.GatewayConfigName)
	gc.Spec.OIDC = &serviceApi.OIDCConfig{
		IssuerURL: "https://keycloak.example.com/realms/test",
		ClientID:  "feast-client",
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "oidc-secret"},
			Key:                  "client-secret",
		},
	}
	gc.Status.Domain = "feast-gateway.apps.example.com"

	cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(auth, gc))
	g.Expect(err).To(Succeed())

	cr, err := handler.NewCRObject(context.Background(), cli, dsc)
	g.Expect(err).To(Succeed())
	feast, ok := cr.(*componentApi.FeastOperator)
	g.Expect(ok).To(BeTrue(), "NewCRObject should return *FeastOperator")
	g.Expect(feast.Spec.OIDC).ShouldNot(BeNil())
	g.Expect(feast.Spec.OIDC.IssuerURL).Should(Equal("https://keycloak.example.com/realms/test"))
}

func TestNewCRObject_OIDC_GatewayStatusDomainNotRequired(t *testing.T) {
	handler := &componentHandler{}
	g := NewWithT(t)
	dsc := createDSCWithFeastOperator(operatorv1.Managed)

	s := feastTestScheme(t)
	auth := createClusterAuth(configv1.AuthenticationType("OIDC"))
	gc := &serviceApi.GatewayConfig{}
	gc.SetName(serviceApi.GatewayConfigName)
	gc.Spec.OIDC = &serviceApi.OIDCConfig{
		IssuerURL: "https://keycloak.example.com/realms/test",
		ClientID:  "feast-client",
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "oidc-secret"},
			Key:                  "client-secret",
		},
	}
	// Status.Domain empty — Feast still gets OIDC issuer; domain is not stored on FeastOperator.
	gc.Status.Domain = ""

	cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(auth, gc))
	g.Expect(err).To(Succeed())

	cr, err := handler.NewCRObject(context.Background(), cli, dsc)
	g.Expect(err).To(Succeed())
	feast, ok := cr.(*componentApi.FeastOperator)
	g.Expect(ok).To(BeTrue(), "NewCRObject should return *FeastOperator")
	g.Expect(feast.Spec.OIDC).ShouldNot(BeNil())
	g.Expect(feast.Spec.OIDC.IssuerURL).Should(Equal("https://keycloak.example.com/realms/test"))
}

func TestNewCRObject_InvalidGatewayConfigOIDCIssuerURL(t *testing.T) {
	handler := &componentHandler{}
	dsc := createDSCWithFeastOperator(operatorv1.Managed)

	s := feastTestScheme(t)
	auth := createClusterAuth(configv1.AuthenticationType("OIDC"))

	tests := []struct {
		name      string
		issuerURL string
	}{
		{name: "http scheme", issuerURL: "http://keycloak.example.com/realms/test"},
		{name: "missing host", issuerURL: "https:///realms/test"},
		{name: "not a URL", issuerURL: "::not-a-uri"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			gc := &serviceApi.GatewayConfig{}
			gc.SetName(serviceApi.GatewayConfigName)
			gc.Spec.OIDC = &serviceApi.OIDCConfig{IssuerURL: tt.issuerURL}

			cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(auth, gc))
			g.Expect(err).To(Succeed())

			_, err = handler.NewCRObject(context.Background(), cli, dsc)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring(serviceApi.GatewayConfigName))
		})
	}
}

func TestNewCRObject_WithoutOIDC(t *testing.T) {
	handler := &componentHandler{}
	g := NewWithT(t)
	dsc := createDSCWithFeastOperator(operatorv1.Managed)

	s := feastTestScheme(t)
	auth := createClusterAuth(configv1.AuthenticationTypeIntegratedOAuth)

	cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(auth))
	g.Expect(err).To(Succeed())

	cr, err := handler.NewCRObject(context.Background(), cli, dsc)
	g.Expect(err).To(Succeed())
	feast, ok := cr.(*componentApi.FeastOperator)
	g.Expect(ok).To(BeTrue(), "NewCRObject should return *FeastOperator")
	g.Expect(feast.Spec.OIDC).Should(BeNil())
}

func createClusterAuth(authType configv1.AuthenticationType) *configv1.Authentication {
	return &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
		Spec:       configv1.AuthenticationSpec{Type: authType},
	}
}

func TestIsEnabled(t *testing.T) {
	handler := &componentHandler{}

	tests := []struct {
		name    string
		state   operatorv1.ManagementState
		matcher gt.GomegaMatcher
	}{
		{
			name:    "should return true when management state is Managed",
			state:   operatorv1.Managed,
			matcher: BeTrue(),
		},
		{
			name:    "should return false when management state is Removed",
			state:   operatorv1.Removed,
			matcher: BeFalse(),
		},
		{
			name:    "should return false when management state is Unmanaged",
			state:   operatorv1.Unmanaged,
			matcher: BeFalse(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dsc := createDSCWithFeastOperator(tt.state)

			g.Expect(
				handler.IsEnabled(dsc),
			).Should(
				tt.matcher,
			)
		})
	}
}

func TestUpdateDSCStatus(t *testing.T) {
	handler := &componentHandler{}

	t.Run("should handle enabled component with ready FeastOperator CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithFeastOperator(operatorv1.Managed)
		feastoperator := createFeastOperatorCR(true)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, feastoperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionTrue))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.feastoperator.managementState == "%s"`, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.ReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is ready"`, ReadyConditionType)),
		))
	})

	t.Run("should handle enabled component with not ready FeastOperator CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithFeastOperator(operatorv1.Managed)
		feastoperator := createFeastOperatorCR(false)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, feastoperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.feastoperator.managementState == "%s"`, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is not ready"`, ReadyConditionType)),
		))
	})

	t.Run("should return ConditionFalse when component CR has deletionTimestamp", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithFeastOperator(operatorv1.Managed)
		feastoperator := createFeastOperatorCR(true)
		now := metav1.Now()
		feastoperator.SetDeletionTimestamp(&now)
		feastoperator.SetFinalizers([]string{"test-finalizer"})

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, feastoperator))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.DeletingReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, ReadyConditionType, status.DeletingMessage),
		)))
	})

	t.Run("should handle disabled component", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithFeastOperator(operatorv1.Removed)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.feastoperator.managementState == "%s"`, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})

	t.Run("should handle empty management state as Removed", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithFeastOperator("")

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.feastoperator.managementState == "%s"`, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})
}

func createDSCWithFeastOperator(managementState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.FeastOperator.ManagementState = managementState

	return &dsc
}

func createFeastOperatorCR(ready bool) *componentApi.FeastOperator {
	c := componentApi.FeastOperator{}
	c.SetGroupVersionKind(gvk.FeastOperator)
	c.SetName(componentApi.FeastOperatorInstanceName)

	if ready {
		c.Status.Conditions = []common.Condition{{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  status.ReadyReason,
			Message: "Component is ready",
		}}
	} else {
		c.Status.Conditions = []common.Condition{{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: "Component is not ready",
		}}
	}

	return &c
}
