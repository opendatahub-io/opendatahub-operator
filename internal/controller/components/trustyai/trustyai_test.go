//nolint:testpackage
package trustyai

import (
	"encoding/json"
	"testing"

	gt "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.TrustyAIComponentName))
}

func TestNewCRObject(t *testing.T) {
	handler := &componentHandler{}

	g := NewWithT(t)
	dsc := createDSCWithTrustyAI(operatorv1.Managed)

	cr := handler.NewCRObject(dsc)
	g.Expect(cr).ShouldNot(BeNil())
	g.Expect(cr).Should(BeAssignableToTypeOf(&componentApi.TrustyAI{}))

	g.Expect(cr).Should(WithTransform(json.Marshal, And(
		jq.Match(`.metadata.name == "%s"`, componentApi.TrustyAIInstanceName),
		jq.Match(`.kind == "%s"`, componentApi.TrustyAIKind),
		jq.Match(`.apiVersion == "%s"`, componentApi.GroupVersion),
		jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
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
			dsc := createDSCWithTrustyAI(tt.state)

			g.Expect(
				handler.IsEnabled(dsc),
			).Should(
				tt.matcher,
			)
		})
	}
}

func TestCreateConfigMap(t *testing.T) {
	t.Run("should create ConfigMap when configuration is provided", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		// Create TrustyAI CR with configuration
		trustyai := createTrustyAICRWithConfig(true, true)
		dsc := createDSCWithTrustyAI(operatorv1.Managed)
		dsciObj := createDSCI("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsciObj))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: trustyai,
			DSCI:     dsciObj,
		}

		err = createConfigMap(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify ConfigMap was added to resources
		g.Expect(rr.Resources).Should(HaveLen(1))
		configMapResource := rr.Resources[0]
		g.Expect(configMapResource.GetName()).Should(Equal("trustyai-dsc-config"))
		g.Expect(configMapResource.GetNamespace()).Should(Equal("test-namespace"))

		// Verify ConfigMap data
		data, found, err := unstructured.NestedStringMap(configMapResource.Object, "data")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(data["eval.lmeval.permitCodeExecution"]).Should(Equal("true"))
		g.Expect(data["eval.lmeval.permitOnline"]).Should(Equal("true"))
	})

	t.Run("should handle partial configuration", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		// Create TrustyAI CR with partial configuration
		trustyai := createTrustyAICRWithPartialConfig(true)
		dsc := createDSCWithTrustyAI(operatorv1.Managed)
		dsciObj := createDSCI("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsciObj))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: trustyai,
			DSCI:     dsciObj,
		}

		err = createConfigMap(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify ConfigMap was added to resources with partial configuration
		g.Expect(rr.Resources).Should(HaveLen(1))
		configMapResource := rr.Resources[0]
		g.Expect(configMapResource.GetName()).Should(Equal("trustyai-dsc-config"))
		g.Expect(configMapResource.GetNamespace()).Should(Equal("test-namespace"))

		// Verify only permitCodeExecution is set to true, permitOnline defaults to false
		data, found, err := unstructured.NestedStringMap(configMapResource.Object, "data")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(data["eval.lmeval.permitCodeExecution"]).Should(Equal("true"))
		g.Expect(data["eval.lmeval.permitOnline"]).Should(Equal("false"))
	})
}

func TestUpdateDSCStatus(t *testing.T) {
	handler := &componentHandler{}

	t.Run("should handle enabled component with ready TrustyAI CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithTrustyAI(operatorv1.Managed)
		trustyai := createTrustyAICR(true)
		dsciObj := createDSCI("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, trustyai, dsciObj))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Test ConfigMap creation with default values
		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: trustyai,
			DSCI:     dsciObj,
		}

		err = createConfigMap(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify ConfigMap was added to resources with default values
		g.Expect(rr.Resources).Should(HaveLen(1))
		configMapResource := rr.Resources[0]
		g.Expect(configMapResource.GetName()).Should(Equal("trustyai-dsc-config"))
		g.Expect(configMapResource.GetNamespace()).Should(Equal("test-namespace"))

		// Verify ConfigMap data has default values (false)
		data, found, err := unstructured.NestedStringMap(configMapResource.Object, "data")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(data["eval.lmeval.permitCodeExecution"]).Should(Equal("false"))
		g.Expect(data["eval.lmeval.permitOnline"]).Should(Equal("false"))

		// Test DSC status update
		cs, err := handler.UpdateDSCStatus(ctx, &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionTrue))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.installedComponents."%s" == true`, LegacyComponentName),
			jq.Match(`.status.components.trustyai.managementState == "%s"`, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.ReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is ready"`, ReadyConditionType)),
		))
	})

	t.Run("should handle enabled component with not ready TrustyAI CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithTrustyAI(operatorv1.Managed)
		trustyai := createTrustyAICR(false)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, trustyai))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.installedComponents."%s" == true`, LegacyComponentName),
			jq.Match(`.status.components.trustyai.managementState == "%s"`, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is not ready"`, ReadyConditionType)),
		))
	})

	t.Run("should handle disabled component", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithTrustyAI(operatorv1.Removed)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.installedComponents."%s" == false`, LegacyComponentName),
			jq.Match(`.status.components.trustyai.managementState == "%s"`, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})

	t.Run("should handle empty management state as Removed", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithTrustyAI("")

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.installedComponents."%s" == false`, LegacyComponentName),
			jq.Match(`.status.components.trustyai.managementState == "%s"`, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})
}

func createDSCWithTrustyAI(managementState operatorv1.ManagementState) *dscv1.DataScienceCluster {
	dsc := dscv1.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.TrustyAI.ManagementState = managementState
	dsc.Status.InstalledComponents = make(map[string]bool)

	return &dsc
}

func createDSCI(applicationsNamespace string) *dsciv1.DSCInitialization {
	dsciObj := dsciv1.DSCInitialization{}
	dsciObj.SetGroupVersionKind(gvk.DSCInitialization)
	dsciObj.SetName("test-dsci")
	dsciObj.Spec.ApplicationsNamespace = applicationsNamespace
	return &dsciObj
}

func createTrustyAICR(ready bool) *componentApi.TrustyAI {
	c := componentApi.TrustyAI{}
	c.SetGroupVersionKind(gvk.TrustyAI)
	c.SetName(componentApi.TrustyAIInstanceName)

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

func createTrustyAICRWithConfig(permitCodeExecution, permitOnline bool) *componentApi.TrustyAI {
	c := createTrustyAICR(true)
	c.Spec.Eval.LMEval.PermitCodeExecution = permitCodeExecution
	c.Spec.Eval.LMEval.PermitOnline = permitOnline
	return c
}

func createTrustyAICRWithPartialConfig(allowCodeExecution bool) *componentApi.TrustyAI {
	c := createTrustyAICR(true)
	c.Spec.Eval.LMEval.PermitCodeExecution = allowCodeExecution
	return c
}
