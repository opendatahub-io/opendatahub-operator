//nolint:testpackage
package modelsasservice

import (
	"encoding/json"
	"testing"

	"github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.ModelsAsServiceComponentName))
}

func TestNewCRObject(t *testing.T) {
	handler := &componentHandler{}
	g := NewWithT(t)

	t.Run("creates CR with default Gateway configuration when DSC has no custom Gateway", func(t *testing.T) {
		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

		cr := handler.NewCRObject(dsc)
		g.Expect(cr).ShouldNot(BeNil())
		g.Expect(cr).Should(BeAssignableToTypeOf(&componentApi.ModelsAsService{}))

		g.Expect(cr).Should(WithTransform(json.Marshal, And(
			jq.Match(`.metadata.name == "%s"`, componentApi.ModelsAsServiceInstanceName),
			jq.Match(`.kind == "%s"`, componentApi.ModelsAsServiceKind),
			jq.Match(`.apiVersion == "%s"`, componentApi.GroupVersion),
			jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, operatorv1.Managed),
			jq.Match(`.spec.gateway.namespace == "%s"`, DefaultGatewayNamespace),
			jq.Match(`.spec.gateway.name == "%s"`, DefaultGatewayName),
		)))
	})

	t.Run("creates CR with custom Gateway configuration from DSC specification", func(t *testing.T) {
		customGateway := componentApi.GatewaySpec{
			Namespace: "custom-gateway-namespace",
			Name:      "custom-gateway-name",
		}
		dsc := createDSCWithMaaSEnabledAndGateway(customGateway)

		cr := handler.NewCRObject(dsc)
		g.Expect(cr).ShouldNot(BeNil())

		maasObj, ok := cr.(*componentApi.ModelsAsService)
		g.Expect(ok).Should(BeTrue())
		g.Expect(maasObj.Spec.Gateway.Namespace).Should(Equal("custom-gateway-namespace"))
		g.Expect(maasObj.Spec.Gateway.Name).Should(Equal("custom-gateway-name"))
	})

	t.Run("propagates management state from DSC to ModelsAsService annotations", func(t *testing.T) {
		testCases := []struct {
			name                    string
			inputManagementState    operatorv1.ManagementState
			expectedManagementState operatorv1.ManagementState
		}{
			{"Managed state", operatorv1.Managed, operatorv1.Managed},
			{"Removed state", operatorv1.Removed, operatorv1.Removed},
			{"Unmanaged state defaults to Removed", operatorv1.Unmanaged, operatorv1.Removed},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, tc.inputManagementState)
				cr := handler.NewCRObject(dsc)

				g.Expect(cr).Should(WithTransform(json.Marshal,
					jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, tc.expectedManagementState),
				))
			})
		}
	})
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

func createDSCWithKServeAndMaaS(kserveState, maasState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	return createDSCWithMaaSGateway(kserveState, maasState, componentApi.GatewaySpec{})
}

func createDSCWithMaaSEnabledAndGateway(gatewaySpec componentApi.GatewaySpec) *dscv2.DataScienceCluster {
	return createDSCWithMaaSGateway(operatorv1.Managed, operatorv1.Managed, gatewaySpec)
}

func createDSCWithMaaSGateway(kserveState, maasState operatorv1.ManagementState, gatewaySpec componentApi.GatewaySpec) *dscv2.DataScienceCluster {
	return &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: kserveState,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: maasState,
							ModelsAsServiceSpec: componentApi.ModelsAsServiceSpec{
								Gateway: gatewaySpec,
							},
						},
					},
				},
			},
		},
	}
}
