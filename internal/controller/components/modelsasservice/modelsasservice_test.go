//nolint:testpackage
package modelsasservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega/types"
	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	operatorv1 "github.com/openshift/api/operator/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	pkgtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const testApplicationsNamespace = "tenant-test-ns"

func testDSCI() *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: testApplicationsNamespace,
		},
	}
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.ModelsAsServiceComponentName))
}

func TestNewCRObject_ReturnsModelsAsServiceWhenEnabled(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}
	dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

	cr, err := handler.NewCRObject(context.Background(), nil, dsc)
	g.Expect(err).To(Succeed())
	g.Expect(cr).ToNot(BeNil())

	mas, ok := cr.(*componentApi.ModelsAsService)
	g.Expect(ok).To(BeTrue())
	g.Expect(mas.GetName()).To(Equal(componentApi.ModelsAsServiceInstanceName))
	g.Expect(mas.GetNamespace()).To(BeEmpty(), "ModelsAsService must be cluster-scoped")
}

func TestNewCRObject_ReturnsNilWhenDisabled(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}
	dsc := createDSCWithKServeAndMaaS(operatorv1.Removed, operatorv1.Managed)

	cr, err := handler.NewCRObject(context.Background(), nil, dsc)
	g.Expect(err).To(Succeed())
	g.Expect(cr).To(BeNil())
}

func TestIsEnabled(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	testCases := []struct {
		name            string
		kserveState     operatorv1.ManagementState
		maasState       operatorv1.ManagementState
		expectedEnabled func() types.GomegaMatcher
	}{
		{"should be enabled when both KServe and MaaS are managed", operatorv1.Managed, operatorv1.Managed, BeTrue},
		{"should be disabled when KServe not managed", operatorv1.Removed, operatorv1.Managed, BeFalse},
		{"should be disabled when KServe managed but MaaS is not enabled", operatorv1.Managed, operatorv1.Removed, BeFalse},
		{"should be disabled when KServe is unmanaged", operatorv1.Unmanaged, operatorv1.Managed, BeFalse},
		{"should be disabled when both KServe and MaaS are unmanaged", operatorv1.Unmanaged, operatorv1.Unmanaged, BeFalse},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dsc := createDSCWithKServeAndMaaS(tc.kserveState, tc.maasState)
			g.Expect(handler.IsEnabled(dsc)).Should(tc.expectedEnabled())
		})
	}
}

func TestUpdateDSCStatus(t *testing.T) {
	handler := &componentHandler{}

	t.Run("should return ConditionFalse when component CR has deletionTimestamp", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		cr := createTenantCR(true)
		now := metav1.Now()
		cr.SetDeletionTimestamp(&now)
		cr.SetFinalizers([]string{"test-finalizer"})

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc, cr))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
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

	for _, tc := range []struct {
		name           string
		tenantReady    *bool
		expectedStatus metav1.ConditionStatus
		expectedReason string
		expectedMsg    string
	}{
		{
			name:           "ready Tenant CR",
			tenantReady:    ptr(true),
			expectedStatus: metav1.ConditionTrue,
			expectedReason: status.ReadyReason,
			expectedMsg:    "Component is ready",
		},
		{
			name:           "not-ready Tenant CR",
			tenantReady:    ptr(false),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: status.NotReadyReason,
			expectedMsg:    "Component is not ready",
		},
		{
			name:           "missing Tenant CR",
			tenantReady:    nil,
			expectedStatus: metav1.ConditionFalse,
			expectedReason: status.NotReadyReason,
			expectedMsg:    "Tenant CR not available yet",
		},
	} {
		t.Run("should handle enabled component with "+tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
			var opts []fakeclient.ClientOpts
			if tc.tenantReady != nil {
				opts = append(opts, fakeclient.WithObjects(testDSCI(), dsc, createTenantCR(*tc.tenantReady)))
			} else {
				opts = append(opts, fakeclient.WithObjects(testDSCI(), dsc))
			}

			cli, err := fakeclient.New(opts...)
			g.Expect(err).ShouldNot(HaveOccurred())

			cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   dsc,
				Conditions: conditions.NewManager(dsc, ReadyConditionType),
			})

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(cs).Should(Equal(tc.expectedStatus))

			g.Expect(dsc).Should(WithTransform(json.Marshal, And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, tc.expectedReason),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, ReadyConditionType, tc.expectedMsg),
			)))
		})
	}

	for _, tc := range []struct {
		name           string
		kserveState    operatorv1.ManagementState
		maasState      operatorv1.ManagementState
		expectedReason string
	}{
		{"disabled via MaaS Removed", operatorv1.Managed, operatorv1.Removed, string(operatorv1.Removed)},
		{"disabled via KServe not managed", operatorv1.Removed, operatorv1.Managed, string(operatorv1.Managed)},
	} {
		t.Run("should show MaaS management state when "+tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			dsc := createDSCWithKServeAndMaaS(tc.kserveState, tc.maasState)

			cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc))
			g.Expect(err).ShouldNot(HaveOccurred())

			cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   dsc,
				Conditions: conditions.NewManager(dsc, ReadyConditionType),
			})

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

			g.Expect(dsc).Should(WithTransform(json.Marshal, And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, tc.expectedReason),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
			)))
		})
	}

	t.Run("should handle Tenant CR with no status conditions", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		cr := &maasv1alpha1.Tenant{}
		cr.SetName(maasv1alpha1.TenantInstanceName)
		cr.SetNamespace(MaaSSubscriptionNamespace)
		cr.APIVersion = maasv1alpha1.GroupVersion.String()
		cr.Kind = maasv1alpha1.TenantKind

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc, cr))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Tenant CR exists but has no ready condition yet"`, ReadyConditionType),
		)))
	})

	t.Run("should report CRD not installed when Tenant CRD is missing", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

		noMatchErr := &apimeta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{
			Group: "maas.opendatahub.io", Version: "v1alpha1", Resource: "tenants",
		}}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(testDSCI(), dsc),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*maasv1alpha1.Tenant); ok {
						return noMatchErr
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Tenant CRD not installed"`, ReadyConditionType),
		)))
	})

	t.Run("should propagate unexpected API errors", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

		cli, err := fakeclient.New(
			fakeclient.WithObjects(testDSCI(), dsc),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*maasv1alpha1.Tenant); ok {
						return errors.New("simulated API server error")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		_, err = handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("failed to get Tenant"))
	})
}

func TestCheckMaaSPrerequisites_AllMet(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	handler := &componentHandler{}

	dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
	gw := createMaaSGatewayWithAnnotations(map[string]string{
		"opendatahub.io/managed":                          "false",
		"security.opendatahub.io/authorino-tls-bootstrap": "true",
	})
	authorino := createAuthorinoWithTLS(true)

	cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc, createTenantCR(true), gw, authorino))
	g.Expect(err).ShouldNot(HaveOccurred())

	cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditions.NewManager(dsc, ReadyConditionType, status.ConditionMaaSPrerequisitesAvailable),
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cs).Should(Equal(metav1.ConditionTrue))

	g.Expect(dsc).Should(WithTransform(json.Marshal, And(
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
			status.ConditionMaaSPrerequisitesAvailable, metav1.ConditionTrue),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`,
			status.ConditionMaaSPrerequisitesAvailable, status.MaaSPrerequisitesMetReason),
	)))
}

func TestCheckMaaSPrerequisites_MissingAnnotations(t *testing.T) {
	handler := &componentHandler{}

	for _, tc := range []struct {
		name             string
		gwAnnotations    map[string]string
		missingSubstring string
	}{
		{
			name:             "missing managed annotation",
			gwAnnotations:    map[string]string{"security.opendatahub.io/authorino-tls-bootstrap": "true"},
			missingSubstring: "opendatahub.io/managed",
		},
		{
			name:             "missing tls-bootstrap annotation",
			gwAnnotations:    map[string]string{"opendatahub.io/managed": "false"},
			missingSubstring: "authorino-tls-bootstrap",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
			gw := createMaaSGatewayWithAnnotations(tc.gwAnnotations)
			authorino := createAuthorinoWithTLS(true)

			cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc, createTenantCR(true), gw, authorino))
			g.Expect(err).ShouldNot(HaveOccurred())

			_, err = handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   dsc,
				Conditions: conditions.NewManager(dsc, ReadyConditionType, status.ConditionMaaSPrerequisitesAvailable),
			})

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(dsc).Should(WithTransform(json.Marshal, And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
					status.ConditionMaaSPrerequisitesAvailable, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`,
					status.ConditionMaaSPrerequisitesAvailable, status.MaaSPrerequisitesNotMetReason),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("%s")`,
					status.ConditionMaaSPrerequisitesAvailable, tc.missingSubstring),
			)))
		})
	}
}

func TestCheckMaaSPrerequisites_AuthorinoTLSNotEnabled(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	handler := &componentHandler{}

	dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
	gw := createMaaSGatewayWithAnnotations(map[string]string{
		"opendatahub.io/managed":                          "false",
		"security.opendatahub.io/authorino-tls-bootstrap": "true",
	})
	authorino := createAuthorinoWithTLS(false)

	cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc, createTenantCR(true), gw, authorino))
	g.Expect(err).ShouldNot(HaveOccurred())

	_, err = handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditions.NewManager(dsc, ReadyConditionType, status.ConditionMaaSPrerequisitesAvailable),
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(dsc).Should(WithTransform(json.Marshal, And(
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
			status.ConditionMaaSPrerequisitesAvailable, metav1.ConditionFalse),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Authorino TLS is not enabled")`,
			status.ConditionMaaSPrerequisitesAvailable),
	)))
}

func TestCheckMaaSPrerequisites_GatewayNotFound(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	handler := &componentHandler{}

	dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
	authorino := createAuthorinoWithTLS(true)

	cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc, createTenantCR(true), authorino))
	g.Expect(err).ShouldNot(HaveOccurred())

	_, err = handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditions.NewManager(dsc, ReadyConditionType, status.ConditionMaaSPrerequisitesAvailable),
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(dsc).Should(WithTransform(json.Marshal, And(
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
			status.ConditionMaaSPrerequisitesAvailable, metav1.ConditionFalse),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("maas-default-gateway not found")`,
			status.ConditionMaaSPrerequisitesAvailable),
	)))
}

func TestCheckMaaSPrerequisites_CRDNotInstalled(t *testing.T) {
	handler := &componentHandler{}

	t.Run("Gateway API CRD", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		authorino := createAuthorinoWithTLS(true)

		noMatchErr := &apimeta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{
			Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways",
		}}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(testDSCI(), dsc, createTenantCR(true), authorino),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*gwapiv1.Gateway); ok {
						return noMatchErr
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		_, err = handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType, status.ConditionMaaSPrerequisitesAvailable),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionMaaSPrerequisitesAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Gateway API CRD is not installed")`,
				status.ConditionMaaSPrerequisitesAvailable),
		)))
	})

	t.Run("Authorino CRD", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		gw := createMaaSGatewayWithAnnotations(map[string]string{
			"opendatahub.io/managed":                          "false",
			"security.opendatahub.io/authorino-tls-bootstrap": "true",
		})

		noMatchErr := &apimeta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{
			Group: "operator.authorino.kuadrant.io", Version: "v1beta1", Resource: "authorinos",
		}}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(testDSCI(), dsc, createTenantCR(true), gw),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if u, ok := list.(*unstructured.UnstructuredList); ok && u.GetKind() == "Authorino" {
						return noMatchErr
					}
					return c.List(ctx, list, opts...)
				},
			}),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		_, err = handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType, status.ConditionMaaSPrerequisitesAvailable),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionMaaSPrerequisitesAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Authorino CRD is not installed")`,
				status.ConditionMaaSPrerequisitesAvailable),
		)))
	})
}

func TestCheckMaaSPrerequisites_DisabledAndNonBlocking(t *testing.T) {
	handler := &componentHandler{}

	t.Run("should not set MaaSPrerequisitesAvailable when MaaS is disabled", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Removed)

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		_, err = handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType, status.ConditionMaaSPrerequisitesAvailable),
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).ShouldNot(WithTransform(json.Marshal,
			jq.Match(`.status.conditions[] | select(.type == "%s")`, status.ConditionMaaSPrerequisitesAvailable),
		))
	})

	t.Run("should not affect ModelsAsServiceReady when prerequisites are not met", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		gw := createMaaSGatewayWithAnnotations(nil)
		authorino := createAuthorinoWithTLS(false)

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc, createTenantCR(true), gw, authorino))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType, status.ConditionMaaSPrerequisitesAvailable),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionTrue))
		g.Expect(dsc).Should(WithTransform(json.Marshal,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				ReadyConditionType, metav1.ConditionTrue),
		))
		g.Expect(dsc).Should(WithTransform(json.Marshal,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionMaaSPrerequisitesAvailable, metav1.ConditionFalse),
		))
	})
}

func createMaaSGatewayWithAnnotations(ann map[string]string) *gwapiv1.Gateway {
	gw := &gwapiv1.Gateway{}
	gw.SetName(DefaultGatewayName)
	gw.SetNamespace(DefaultGatewayNamespace)
	gw.SetAnnotations(ann)
	gw.Spec.GatewayClassName = "openshift-default"
	return gw
}

func createAuthorinoWithTLS(tlsEnabled bool) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "operator.authorino.kuadrant.io/v1beta1",
			"kind":       "Authorino",
			"metadata": map[string]any{
				"name":      "authorino",
				"namespace": "kuadrant-system",
			},
			"spec": map[string]any{
				"listener": map[string]any{
					"tls": map[string]any{
						"enabled": tlsEnabled,
					},
				},
			},
		},
	}
}

func createDSCWithKServeAndMaaS(kserveState, maasState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.Kserve.ManagementState = kserveState
	dsc.Spec.Components.Kserve.ModelsAsService.ManagementState = maasState

	return &dsc
}

func createTenantCR(ready bool) *maasv1alpha1.Tenant {
	c := &maasv1alpha1.Tenant{}
	c.SetName(maasv1alpha1.TenantInstanceName)
	c.SetNamespace(MaaSSubscriptionNamespace)
	c.APIVersion = maasv1alpha1.GroupVersion.String()
	c.Kind = maasv1alpha1.TenantKind
	now := metav1.Now()
	if ready {
		c.Status.Conditions = []metav1.Condition{{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             status.ReadyReason,
			Message:            "Component is ready",
			LastTransitionTime: now,
		}}
	} else {
		c.Status.Conditions = []metav1.Condition{{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             status.NotReadyReason,
			Message:            "Component is not ready",
			LastTransitionTime: now,
		}}
	}

	return c
}

func ptr[T any](v T) *T { return &v }

// TestApplyImageOverridesFromParams verifies that the Option B image override
// pipeline (kustomize build → ImageTagTransformerPlugin) correctly replaces the
// default :latest image with a pinned image reference from params.env.
// This is the exact flow that runs in CI via buildMaasOperatorInstallManifests.
func TestApplyImageOverridesFromParams(t *testing.T) {
	g := NewWithT(t)

	manifestsRoot := findManifestsRoot(t)
	kPath := filepath.Join(manifestsRoot, MaasManifestContextDir, "base", "maas-controller", "default")

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()
	resMap, err := k.Run(fs, kPath)
	g.Expect(err).ShouldNot(HaveOccurred(), "kustomize build should succeed")

	// Before override: the images transformer renders the default maas-controller image
	// (tag may be :latest locally or :odh-stable in CI depending on manifest state).
	depBefore := findDeploymentImage(g, resMap)
	g.Expect(depBefore).To(HavePrefix("quay.io/opendatahub/maas-controller:"),
		"kustomize images transformer should produce the default maas-controller image")

	// Write a temporary params.env with a pinned digest to simulate what
	// ApplyParams does when RELATED_IMAGE_* env vars are set in CI.
	pinnedImage := "registry.redhat.io/rhoai/odh-maas-controller-rhel9@sha256:abc123def456"
	tmpDir := t.TempDir()
	paramsContent := "maas-controller-image=" + pinnedImage + "\n"
	err = os.WriteFile(filepath.Join(tmpDir, "params.env"), []byte(paramsContent), 0o600)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = applyImageOverridesFromParams(resMap, filepath.Join(tmpDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred(), "applyImageOverridesFromParams should succeed")

	// After override: the image must be the pinned digest, not the default.
	depAfter := findDeploymentImage(g, resMap)
	g.Expect(depAfter).To(Equal(pinnedImage),
		"ImageTagTransformerPlugin must replace the default image with the pinned digest from params.env")
	g.Expect(depAfter).NotTo(Equal(depBefore),
		"override must actually change the image")
}

// TestApplyImageOverridesFromParams_TagFormat verifies tag-style overrides (repo:tag).
func TestApplyImageOverridesFromParams_TagFormat(t *testing.T) {
	g := NewWithT(t)

	manifestsRoot := findManifestsRoot(t)
	kPath := filepath.Join(manifestsRoot, MaasManifestContextDir, "base", "maas-controller", "default")

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := k.Run(filesys.MakeFsOnDisk(), kPath)
	g.Expect(err).ShouldNot(HaveOccurred())

	pinnedImage := "quay.io/myorg/maas-controller:v1.2.3"
	tmpDir := t.TempDir()
	err = os.WriteFile(filepath.Join(tmpDir, "params.env"),
		[]byte("maas-controller-image="+pinnedImage+"\n"), 0o600)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = applyImageOverridesFromParams(resMap, filepath.Join(tmpDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred())

	depAfter := findDeploymentImage(g, resMap)
	g.Expect(depAfter).To(Equal(pinnedImage))
}

// TestApplyImageOverridesFromParams_NoOverride verifies no-op when params.env
// has no maas-controller-image key.
func TestApplyImageOverridesFromParams_NoOverride(t *testing.T) {
	g := NewWithT(t)

	manifestsRoot := findManifestsRoot(t)
	kPath := filepath.Join(manifestsRoot, MaasManifestContextDir, "base", "maas-controller", "default")

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := k.Run(filesys.MakeFsOnDisk(), kPath)
	g.Expect(err).ShouldNot(HaveOccurred())

	tmpDir := t.TempDir()
	err = os.WriteFile(filepath.Join(tmpDir, "params.env"),
		[]byte("some-other-key=somevalue\n"), 0o600)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = applyImageOverridesFromParams(resMap, filepath.Join(tmpDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred())

	depAfter := findDeploymentImage(g, resMap)
	g.Expect(depAfter).To(HavePrefix("quay.io/opendatahub/maas-controller:"),
		"image should remain unchanged when params.env has no controller image key")
}

// findManifestsRoot walks up from the test file to locate the opt/manifests directory.
func findManifestsRoot(t *testing.T) string {
	t.Helper()
	// Start from the module root (go test runs from the package directory)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot get working directory: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "opt", "manifests")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("opt/manifests not found; skipping (manifests not downloaded)")
		}
		dir = parent
	}
}

// findDeploymentImage extracts the image of the "manager" container from the
// maas-controller Deployment in the rendered resmap.
func findDeploymentImage(g Gomega, rm resmap.ResMap) string {
	for _, r := range rm.Resources() {
		if r.GetKind() != "Deployment" || r.GetName() != MaasControllerDeploymentName {
			continue
		}
		m, err := r.Map()
		g.Expect(err).ShouldNot(HaveOccurred())

		containers, found := nestedSlice(m, "spec", "template", "spec", "containers")
		g.Expect(found).To(BeTrue(), "containers field must exist")

		for _, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if cm["name"] == "manager" {
				img, ok := cm["image"].(string)
				g.Expect(ok).To(BeTrue(), "image must be a string")
				return img
			}
		}
	}
	g.Expect(false).To(BeTrue(), fmt.Sprintf("Deployment %s with container manager not found in resMap", MaasControllerDeploymentName))
	return ""
}

func nestedSlice(obj map[string]any, fields ...string) ([]any, bool) {
	var current any = obj
	for _, f := range fields {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[f]
		if !ok {
			return nil, false
		}
	}
	s, ok := current.([]any)
	return s, ok
}
