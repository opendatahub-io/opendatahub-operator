//nolint:testpackage
package trustyai

import (
	"context"
	"encoding/json"
	"testing"

	gt "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
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

	cr, err := handler.NewCRObject(context.Background(), nil, dsc)
	g.Expect(err).To(Succeed())
	g.Expect(cr).ShouldNot(BeNil())
	g.Expect(cr).Should(BeAssignableToTypeOf(&componentApi.TrustyAI{}))

	g.Expect(cr).Should(WithTransform(json.Marshal, And(
		jq.Match(`.metadata.name == "%s"`, componentApi.TrustyAIInstanceName),
		jq.Match(`.kind == "%s"`, componentApi.TrustyAIKind),
		jq.Match(`.apiVersion == "%s"`, componentApi.GroupVersion),
		jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func TestEvalCRObjectSerialization(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	// Create DSC with no eval fields specified
	dsc := createDSCWithTrustyAI(operatorv1.Managed)

	cr, err := handler.NewCRObject(context.Background(), nil, dsc)
	g.Expect(err).To(Succeed())
	g.Expect(cr).ShouldNot(BeNil())

	// Test JSON serialization to ensure required fields are present
	trustyaiCR, ok := cr.(*componentApi.TrustyAI)
	g.Expect(ok).Should(BeTrue(), "Expected cr to be of type *componentApi.TrustyAI")
	g.Expect(trustyaiCR).Should(WithTransform(json.Marshal, And(
		jq.Match(`.spec.eval.lmeval.permitCodeExecution == "%s"`, EvalPermissionDeny),
		jq.Match(`.spec.eval.lmeval.permitOnline == "%s"`, EvalPermissionDeny),
		jq.Match(`.spec.eval.lmeval | has("permitCodeExecution")`),
		jq.Match(`.spec.eval.lmeval | has("permitOnline")`),
	)))
}

func TestMCPGuardrailsModeCRObjectSerialization(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	// Create DSC with MCPGuardrailsMode enabled
	dsc := createDSCWithTrustyAI(operatorv1.Managed)
	dsc.Spec.Components.TrustyAI.MCPGuardrailsMode = true

	cr, err := handler.NewCRObject(context.Background(), nil, dsc)
	g.Expect(err).To(Succeed())
	g.Expect(cr).ShouldNot(BeNil())

	// Test JSON serialization to ensure required fields are present
	trustyaiCR, ok := cr.(*componentApi.TrustyAI)
	g.Expect(ok).Should(BeTrue(), "Expected cr to be of type *componentApi.TrustyAI")
	g.Expect(trustyaiCR).Should(WithTransform(json.Marshal, And(
		jq.Match(`.spec.mcpGuardrailsMode == true`),
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
			jq.Match(`.status.components.trustyai.managementState == "%s"`, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is not ready"`, ReadyConditionType)),
		))
	})

	t.Run("should return ConditionFalse when component CR has deletionTimestamp", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithTrustyAI(operatorv1.Managed)
		trustyai := createTrustyAICR(true)
		now := metav1.Now()
		trustyai.SetDeletionTimestamp(&now)
		trustyai.SetFinalizers([]string{"test-finalizer"})

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
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.DeletingReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, ReadyConditionType, status.DeletingMessage),
		)))
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
			jq.Match(`.status.components.trustyai.managementState == "%s"`, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})
}

func TestInitializeWithMCPGuardrailsMode(t *testing.T) {
	t.Run("should add mcp-guardrails manifest when MCPGuardrailsMode is enabled", func(t *testing.T) {
		g := NewWithT(t)
		trustyai := createTrustyAICRWithMCPGuardrailsMode(true)
		rr := &odhtypes.ReconciliationRequest{
			Instance: trustyai,
			Release:  common.Release{Name: cluster.OpenDataHub},
		}
		err := initialize(context.Background(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Manifests).Should(HaveLen(1))
		g.Expect(rr.Manifests[0].SourcePath).Should(Equal("/overlays/mcp-guardrails"))
	})

	t.Run("should not add mcp-guardrails manifest when MCPGuardrailsMode is disabled", func(t *testing.T) {
		g := NewWithT(t)
		trustyai := createTrustyAICRWithMCPGuardrailsMode(false)
		rr := &odhtypes.ReconciliationRequest{
			Instance: trustyai,
			Release:  common.Release{Name: cluster.OpenDataHub},
		}
		err := initialize(context.Background(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Manifests).Should(HaveLen(1))
		g.Expect(rr.Manifests[0].SourcePath).Should(Equal(manifestsPath("", cluster.OpenDataHub).SourcePath))
	})
}

func createDSCWithTrustyAI(managementState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.TrustyAI.ManagementState = managementState

	return &dsc
}

func createDSCI(applicationsNamespace string) *dsciv2.DSCInitialization {
	dsciObj := dsciv2.DSCInitialization{}
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
	if permitCodeExecution {
		c.Spec.Eval.LMEval.PermitCodeExecution = EvalPermissionAllow
	} else {
		c.Spec.Eval.LMEval.PermitCodeExecution = EvalPermissionDeny
	}
	if permitOnline {
		c.Spec.Eval.LMEval.PermitOnline = EvalPermissionAllow
	} else {
		c.Spec.Eval.LMEval.PermitOnline = EvalPermissionDeny
	}
	return c
}

func createTrustyAICRWithPartialConfig(allowCodeExecution bool) *componentApi.TrustyAI {
	c := createTrustyAICR(true)
	if allowCodeExecution {
		c.Spec.Eval.LMEval.PermitCodeExecution = EvalPermissionAllow
	} else {
		c.Spec.Eval.LMEval.PermitCodeExecution = EvalPermissionDeny
	}
	return c
}

func createTrustyAICRWithMCPGuardrailsMode(mcpGuardrailsMode bool) *componentApi.TrustyAI {
	c := createTrustyAICR(true)
	c.Spec.MCPGuardrailsMode = mcpGuardrailsMode
	return c
}

func createTrustyAIDeployment(selectorLabels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustyaiDeploymentName,
			Namespace: testNS,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: selectorLabels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Image: "test"}}},
			},
		},
	}
}

func makeCRD(name string, lbls map[string]string) *extv1.CustomResourceDefinition {
	crd := &extv1.CustomResourceDefinition{}
	crd.SetName(name)
	crd.SetLabels(lbls)
	return crd
}

func odhKserveLabel() map[string]string {
	return map[string]string{
		labels.ODH.Component(componentApi.KserveComponentName): labels.True,
	}
}

// TestIsInferenceServicesCRD covers the DeleteFunc predicate (full name + label check).
func TestIsInferenceServicesCRD(t *testing.T) {
	tests := []struct {
		name     string
		crdName  string
		lbls     map[string]string
		expected bool
	}{
		{
			name:     "correct name and ODH label → true",
			crdName:  InferenceServicesCRDName,
			lbls:     odhKserveLabel(),
			expected: true,
		},
		{
			name:     "correct name but no ODH label → false",
			crdName:  InferenceServicesCRDName,
			lbls:     nil,
			expected: false,
		},
		{
			name:     "wrong name with ODH label → false",
			crdName:  "other.crd.io",
			lbls:     odhKserveLabel(),
			expected: false,
		},
		{
			name:     "wrong name no label → false",
			crdName:  "other.crd.io",
			lbls:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			crd := makeCRD(tt.crdName, tt.lbls)
			g.Expect(isInferenceServicesCRD(crd)).Should(Equal(tt.expected))
		})
	}
}

// TestInferenceServicesCRDCreatePredicate covers the CreateFunc predicate (name-only check).
// KServe creates the CRD without the ODH label; the predicate must fire on name match alone.
func TestInferenceServicesCRDCreatePredicate(t *testing.T) {
	createFired := func(crd *extv1.CustomResourceDefinition) bool {
		return crd.GetName() == InferenceServicesCRDName
	}

	tests := []struct {
		name     string
		crdName  string
		lbls     map[string]string
		expected bool
	}{
		{
			name:     "correct name, no ODH label (KServe-created CRD) → fires",
			crdName:  InferenceServicesCRDName,
			lbls:     nil,
			expected: true,
		},
		{
			name:     "correct name, with ODH label → fires",
			crdName:  InferenceServicesCRDName,
			lbls:     odhKserveLabel(),
			expected: true,
		},
		{
			name:     "wrong name → does not fire",
			crdName:  "other.crd.io",
			lbls:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			crd := makeCRD(tt.crdName, tt.lbls)
			g.Expect(createFired(crd)).Should(Equal(tt.expected))
		})
	}
}

// TestInferenceServicesCRDUpdatePredicate covers the UpdateFunc predicate (label-added transition).
func TestInferenceServicesCRDUpdatePredicate(t *testing.T) {
	odhLabel := labels.ODH.Component(componentApi.KserveComponentName)

	updateFired := func(oldObj, newObj *extv1.CustomResourceDefinition) bool {
		if newObj.GetName() != InferenceServicesCRDName {
			return false
		}
		wasLabeled := oldObj.GetLabels()[odhLabel] == labels.True
		isLabeled := newObj.GetLabels()[odhLabel] == labels.True
		return !wasLabeled && isLabeled
	}

	tests := []struct {
		name     string
		crdName  string
		oldLbls  map[string]string
		newLbls  map[string]string
		expected bool
	}{
		{
			name:     "ODH label added (absent → present) → fires",
			crdName:  InferenceServicesCRDName,
			oldLbls:  nil,
			newLbls:  odhKserveLabel(),
			expected: true,
		},
		{
			name:     "ODH label already present → does not fire (no spurious reconciliation)",
			crdName:  InferenceServicesCRDName,
			oldLbls:  odhKserveLabel(),
			newLbls:  odhKserveLabel(),
			expected: false,
		},
		{
			name:     "ODH label removed (present → absent) → does not fire",
			crdName:  InferenceServicesCRDName,
			oldLbls:  odhKserveLabel(),
			newLbls:  nil,
			expected: false,
		},
		{
			name:     "neither old nor new has ODH label → does not fire",
			crdName:  InferenceServicesCRDName,
			oldLbls:  nil,
			newLbls:  nil,
			expected: false,
		},
		{
			name:     "wrong CRD name with label added → does not fire",
			crdName:  "other.crd.io",
			oldLbls:  nil,
			newLbls:  odhKserveLabel(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			oldCRD := makeCRD(tt.crdName, tt.oldLbls)
			newCRD := makeCRD(tt.crdName, tt.newLbls)
			g.Expect(updateFired(oldCRD, newCRD)).Should(Equal(tt.expected))
		})
	}
}

const testNS = "redhat-ods-applications"

func TestMigrateDeploymentSelector(t *testing.T) {
	t.Run("should delete Deployment with old control-plane selector value", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		staleSelector := map[string]string{controlPlaneLabelKey: "controller-manager"}
		deploy := createTrustyAIDeployment(staleSelector)
		dsci := createDSCI(testNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(deploy, dsci))
		g.Expect(err).To(Succeed())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		err = migrateDeploymentSelector(ctx, rr)
		g.Expect(err).To(Succeed())

		result := &appsv1.Deployment{}
		err = cli.Get(ctx, client.ObjectKey{Name: trustyaiDeploymentName, Namespace: testNS}, result)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(client.IgnoreNotFound(err)).To(Succeed())
	})

	t.Run("should delete Deployment missing app.kubernetes.io/part-of label", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		selectorMissingPartOf := map[string]string{controlPlaneLabelKey: controlPlaneLabelValue}
		deploy := createTrustyAIDeployment(selectorMissingPartOf)
		dsci := createDSCI(testNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(deploy, dsci))
		g.Expect(err).To(Succeed())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		err = migrateDeploymentSelector(ctx, rr)
		g.Expect(err).To(Succeed())

		result := &appsv1.Deployment{}
		err = cli.Get(ctx, client.ObjectKey{Name: trustyaiDeploymentName, Namespace: testNS}, result)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(client.IgnoreNotFound(err)).To(Succeed())
	})

	t.Run("should not delete Deployment with correct selector", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		correctSelector := map[string]string{
			controlPlaneLabelKey: controlPlaneLabelValue,
			partOfLabelKey:       partOfLabelValue,
		}
		deploy := createTrustyAIDeployment(correctSelector)
		dsci := createDSCI(testNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(deploy, dsci))
		g.Expect(err).To(Succeed())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		err = migrateDeploymentSelector(ctx, rr)
		g.Expect(err).To(Succeed())

		result := &appsv1.Deployment{}
		err = cli.Get(ctx, client.ObjectKey{Name: trustyaiDeploymentName, Namespace: testNS}, result)
		g.Expect(err).To(Succeed())
	})

	t.Run("should not delete Deployment with correct labels plus extra labels", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		selectorWithExtra := map[string]string{
			controlPlaneLabelKey:          controlPlaneLabelValue,
			partOfLabelKey:                partOfLabelValue,
			"app.opendatahub.io/trustyai": "true",
		}
		deploy := createTrustyAIDeployment(selectorWithExtra)
		dsci := createDSCI(testNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(deploy, dsci))
		g.Expect(err).To(Succeed())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		err = migrateDeploymentSelector(ctx, rr)
		g.Expect(err).To(Succeed())

		result := &appsv1.Deployment{}
		err = cli.Get(ctx, client.ObjectKey{Name: trustyaiDeploymentName, Namespace: testNS}, result)
		g.Expect(err).To(Succeed())
	})

	t.Run("should delete Deployment with nil MatchLabels", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		deploy := createTrustyAIDeployment(nil)
		deploy.Spec.Selector = &metav1.LabelSelector{MatchLabels: nil}
		dsci := createDSCI(testNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(deploy, dsci))
		g.Expect(err).To(Succeed())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		err = migrateDeploymentSelector(ctx, rr)
		g.Expect(err).To(Succeed())

		result := &appsv1.Deployment{}
		err = cli.Get(ctx, client.ObjectKey{Name: trustyaiDeploymentName, Namespace: testNS}, result)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(client.IgnoreNotFound(err)).To(Succeed())
	})

	t.Run("should delete Deployment with empty MatchLabels", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		deploy := createTrustyAIDeployment(map[string]string{})
		dsci := createDSCI(testNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(deploy, dsci))
		g.Expect(err).To(Succeed())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		err = migrateDeploymentSelector(ctx, rr)
		g.Expect(err).To(Succeed())

		result := &appsv1.Deployment{}
		err = cli.Get(ctx, client.ObjectKey{Name: trustyaiDeploymentName, Namespace: testNS}, result)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(client.IgnoreNotFound(err)).To(Succeed())
	})

	t.Run("should delete Deployment with nil selector", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		deploy := createTrustyAIDeployment(nil)
		deploy.Spec.Selector = nil
		dsci := createDSCI(testNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(deploy, dsci))
		g.Expect(err).To(Succeed())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		err = migrateDeploymentSelector(ctx, rr)
		g.Expect(err).To(Succeed())

		result := &appsv1.Deployment{}
		err = cli.Get(ctx, client.ObjectKey{Name: trustyaiDeploymentName, Namespace: testNS}, result)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(client.IgnoreNotFound(err)).To(Succeed())
	})

	t.Run("should be no-op when Deployment does not exist", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsci := createDSCI(testNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
		g.Expect(err).To(Succeed())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		err = migrateDeploymentSelector(ctx, rr)
		g.Expect(err).To(Succeed())
	})
}
