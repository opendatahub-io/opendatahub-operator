//nolint:testpackage,dupl
package modelcontroller

import (
	"context"
	"encoding/json"
	"testing"

	gt "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	// side import for component registry.
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kserve"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelmeshserving"

	. "github.com/onsi/gomega"
)

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.ModelControllerComponentName))
}

func TestNewCRObject(t *testing.T) {
	handler := &componentHandler{}

	tests := []struct {
		name                    string
		kserveState             operatorv1.ManagementState
		modelmeshservingState   operatorv1.ManagementState
		modelregistryState      operatorv1.ManagementState
		expectedManagementState operatorv1.ManagementState
		expectedKserveState     operatorv1.ManagementState
		expectedModelmeshState  operatorv1.ManagementState
		expectedModelregState   operatorv1.ManagementState
	}{
		{
			name:                    "should create ModelController CR when KServe is Managed",
			kserveState:             operatorv1.Managed,
			modelmeshservingState:   operatorv1.Removed,
			modelregistryState:      operatorv1.Removed,
			expectedManagementState: operatorv1.Managed,
			expectedKserveState:     operatorv1.Managed,
			expectedModelmeshState:  operatorv1.Removed,
			expectedModelregState:   operatorv1.Removed,
		},
		{
			name:                    "should create ModelController CR when ModelMeshServing is Managed",
			kserveState:             operatorv1.Removed,
			modelmeshservingState:   operatorv1.Managed,
			modelregistryState:      operatorv1.Removed,
			expectedManagementState: operatorv1.Managed,
			expectedKserveState:     operatorv1.Removed,
			expectedModelmeshState:  operatorv1.Managed,
			expectedModelregState:   operatorv1.Removed,
		},
		{
			name:                    "should create ModelController CR when both KServe and ModelMeshServing are Managed",
			kserveState:             operatorv1.Managed,
			modelmeshservingState:   operatorv1.Managed,
			modelregistryState:      operatorv1.Managed,
			expectedManagementState: operatorv1.Managed,
			expectedKserveState:     operatorv1.Managed,
			expectedModelmeshState:  operatorv1.Managed,
			expectedModelregState:   operatorv1.Managed,
		},
		{
			name:                    "should create ModelController CR as Removed when no dependencies are Managed",
			kserveState:             operatorv1.Removed,
			modelmeshservingState:   operatorv1.Removed,
			modelregistryState:      operatorv1.Removed,
			expectedManagementState: operatorv1.Removed,
			expectedKserveState:     operatorv1.Removed,
			expectedModelmeshState:  operatorv1.Removed,
			expectedModelregState:   operatorv1.Removed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dsc := createDSCWithModelController(tt.kserveState, tt.modelmeshservingState, tt.modelregistryState)

			cr := handler.NewCRObject(dsc)
			g.Expect(cr).ShouldNot(BeNil())
			g.Expect(cr).Should(BeAssignableToTypeOf(&componentApi.ModelController{}))

			g.Expect(cr).Should(WithTransform(json.Marshal, And(
				jq.Match(`.metadata.name == "%s"`, componentApi.ModelControllerInstanceName),
				jq.Match(`.kind == "%s"`, componentApi.ModelControllerKind),
				jq.Match(`.apiVersion == "%s"`, componentApi.GroupVersion),
				jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, tt.expectedManagementState),
				jq.Match(`.spec.kserve.managementState == "%s"`, tt.expectedKserveState),
				jq.Match(`.spec.modelMeshServing.managementState == "%s"`, tt.expectedModelmeshState),
				jq.Match(`.spec.modelRegistry.managementState == "%s"`, tt.expectedModelregState),
			)))
		})
	}
}

func TestIsEnabled(t *testing.T) {
	handler := &componentHandler{}

	tests := []struct {
		name                  string
		kserveState           operatorv1.ManagementState
		modelmeshservingState operatorv1.ManagementState
		matcher               gt.GomegaMatcher
	}{
		{
			name:                  "should return true when KServe is Managed",
			kserveState:           operatorv1.Managed,
			modelmeshservingState: operatorv1.Removed,
			matcher:               BeTrue(),
		},
		{
			name:                  "should return true when ModelMeshServing is Managed",
			kserveState:           operatorv1.Removed,
			modelmeshservingState: operatorv1.Managed,
			matcher:               BeTrue(),
		},
		{
			name:                  "should return true when both KServe and ModelMeshServing are Managed",
			kserveState:           operatorv1.Managed,
			modelmeshservingState: operatorv1.Managed,
			matcher:               BeTrue(),
		},
		{
			name:                  "should return false when both KServe and ModelMeshServing are Removed",
			kserveState:           operatorv1.Removed,
			modelmeshservingState: operatorv1.Removed,
			matcher:               BeFalse(),
		},
		{
			name:                  "should return false when KServe is Unmanaged and ModelMeshServing is Removed",
			kserveState:           operatorv1.Unmanaged,
			modelmeshservingState: operatorv1.Removed,
			matcher:               BeFalse(),
		},
		{
			name:                  "should return false when KServe is Removed and ModelMeshServing is Unmanaged",
			kserveState:           operatorv1.Removed,
			modelmeshservingState: operatorv1.Unmanaged,
			matcher:               BeFalse(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dsc := createDSCWithModelController(tt.kserveState, tt.modelmeshservingState, operatorv1.Removed)

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

	t.Run("should handle enabled component with ready ModelController CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		dsc := createDSCWithModelController(operatorv1.Managed, operatorv1.Removed, operatorv1.Removed)
		modelcontroller := createModelControllerCR(true)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, modelcontroller))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionTrue))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.ReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is ready"`, ReadyConditionType)),
		))
	})

	t.Run("should handle enabled component with not ready ModelController CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		dsc := createDSCWithModelController(operatorv1.Removed, operatorv1.Managed, operatorv1.Removed)
		modelcontroller := createModelControllerCR(false)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, modelcontroller))
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
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is not ready"`, ReadyConditionType)),
		))
	})

	t.Run("should handle disabled component", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		dsc := createDSCWithModelController(operatorv1.Removed, operatorv1.Removed, operatorv1.Removed)

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
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})
}

func createDSCWithModelController(kserveState, modelmeshservingState, modelregistryState operatorv1.ManagementState) *dscv1.DataScienceCluster {
	dsc := dscv1.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.Kserve.ManagementState = kserveState
	dsc.Spec.Components.ModelMeshServing.ManagementState = modelmeshservingState
	dsc.Spec.Components.ModelRegistry.ManagementState = modelregistryState
	dsc.Status.InstalledComponents = make(map[string]bool)

	return &dsc
}

func createModelControllerCR(ready bool) *componentApi.ModelController {
	c := componentApi.ModelController{}
	c.SetGroupVersionKind(gvk.ModelController)
	c.SetName(componentApi.ModelControllerInstanceName)

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
