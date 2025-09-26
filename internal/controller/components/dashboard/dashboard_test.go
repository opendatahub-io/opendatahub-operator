package dashboard_test

import (
	"encoding/json"
	"testing"

	gt "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// assertDashboardManagedState verifies the expected relationship between ManagementState,
// presence of Dashboard CR, and InstalledComponents status for the Dashboard component.
// When ManagementState is Managed and no Dashboard CR exists, InstalledComponents should be false.
// When ManagementState is Unmanaged/Removed, the component should not be actively managed.
func assertDashboardManagedState(t *testing.T, dsc *dscv1.DataScienceCluster, state operatorv1.ManagementState) {
	t.Helper()
	g := NewWithT(t)

	if state == operatorv1.Managed {
		// For Managed state, component should be enabled but not ready (no Dashboard CR)
		// Note: InstalledComponents will be true when Dashboard CR exists regardless of Ready status
		g.Expect(dsc.Status.Components.Dashboard.ManagementState).Should(Equal(operatorv1.Managed))
		// When ManagementState is Managed and no Dashboard CR exists, InstalledComponents should be false
		g.Expect(dsc.Status.InstalledComponents[dashboard.LegacyComponentNameUpstream]).Should(BeFalse())
	} else {
		// For Unmanaged and Removed states, component should not be actively managed
		g.Expect(dsc.Status.InstalledComponents[dashboard.LegacyComponentNameUpstream]).Should(BeFalse())
		g.Expect(dsc.Status.Components.Dashboard.ManagementState).Should(Equal(state))
		g.Expect(dsc.Status.Components.Dashboard.DashboardCommonStatus).Should(BeNil())
	}
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &dashboard.ComponentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.DashboardComponentName))
}

func TestNewCRObject(t *testing.T) {
	handler := &dashboard.ComponentHandler{}

	g := NewWithT(t)
	dsc := createDSCWithDashboard(operatorv1.Managed)

	cr := handler.NewCRObject(dsc)
	g.Expect(cr).ShouldNot(BeNil())
	g.Expect(cr).Should(BeAssignableToTypeOf(&componentApi.Dashboard{}))

	g.Expect(cr).Should(WithTransform(json.Marshal, And(
		jq.Match(`.metadata.name == "%s"`, componentApi.DashboardInstanceName),
		jq.Match(`.kind == "%s"`, componentApi.DashboardKind),
		jq.Match(`.apiVersion == "%s"`, componentApi.GroupVersion),
		jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func TestIsEnabled(t *testing.T) {
	handler := &dashboard.ComponentHandler{}

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
			dsc := createDSCWithDashboard(tt.state)

			g.Expect(
				handler.IsEnabled(dsc),
			).Should(
				tt.matcher,
			)
		})
	}
}

func TestUpdateDSCStatus(t *testing.T) {
	handler := &dashboard.ComponentHandler{}

	t.Run("enabled component with ready Dashboard CR", func(t *testing.T) {
		testEnabledComponentWithReadyCR(t, handler)
	})

	t.Run("enabled component with not ready Dashboard CR", func(t *testing.T) {
		testEnabledComponentWithNotReadyCR(t, handler)
	})

	t.Run("disabled component", func(t *testing.T) {
		testDisabledComponent(t, handler)
	})

	t.Run("empty management state as Removed", func(t *testing.T) {
		testEmptyManagementState(t, handler)
	})

	t.Run("Dashboard CR not found", func(t *testing.T) {
		testDashboardCRNotFound(t, handler)
	})

	t.Run("invalid Instance type", func(t *testing.T) {
		testInvalidInstanceType(t, handler)
	})

	t.Run("Dashboard CR without Ready condition", func(t *testing.T) {
		testDashboardCRWithoutReadyCondition(t, handler)
	})

	t.Run("Dashboard CR with Ready Condition True", func(t *testing.T) {
		testDashboardCRWithReadyConditionTrue(t, handler)
	})

	t.Run("different management states", func(t *testing.T) {
		testDifferentManagementStates(t, handler)
	})

	t.Run("nil Client", func(t *testing.T) {
		testNilClient(t, handler)
	})

	t.Run("nil Instance", func(t *testing.T) {
		testNilInstance(t, handler)
	})
}

// testEnabledComponentWithReadyCR tests the enabled component with ready Dashboard CR scenario.
func testEnabledComponentWithReadyCR(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	testEnabledComponentWithCR(t, handler, true, metav1.ConditionTrue, status.ReadyReason, "Component is ready")
}

// testEnabledComponentWithNotReadyCR tests the enabled component with not ready Dashboard CR scenario.
func testEnabledComponentWithNotReadyCR(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	testEnabledComponentWithCR(t, handler, false, metav1.ConditionFalse, status.NotReadyReason, "Component is not ready")
}

// testEnabledComponentWithCR is a helper function that tests the enabled component with a Dashboard CR.
func testEnabledComponentWithCR(t *testing.T, handler *dashboard.ComponentHandler, isReady bool, expectedStatus metav1.ConditionStatus, expectedReason, expectedMessage string) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	dsc := createDSCWithDashboard(operatorv1.Managed)
	dashboardInstance := createDashboardCR(isReady)

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dashboardInstance))
	g.Expect(err).ShouldNot(HaveOccurred())

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       dsci,
		Conditions: conditions.NewManager(dsc, dashboard.ReadyConditionType),
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cs).Should(Equal(expectedStatus))

	g.Expect(dsc).Should(WithTransform(json.Marshal, And(
		jq.Match(`.status.installedComponents."%s" == true`, dashboard.LegacyComponentNameUpstream),
		jq.Match(`.status.components.dashboard.managementState == "%s"`, operatorv1.Managed),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, dashboard.ReadyConditionType, expectedStatus),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, dashboard.ReadyConditionType, expectedReason),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, dashboard.ReadyConditionType, expectedMessage)),
	))
}

// testDisabledComponent tests the disabled component scenario.
func testDisabledComponent(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	dsc := createDSCWithDashboard(operatorv1.Removed)

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
	g.Expect(err).ShouldNot(HaveOccurred())

	cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       dsci,
		Conditions: conditions.NewManager(dsc, dashboard.ReadyConditionType),
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

	g.Expect(dsc).Should(WithTransform(json.Marshal, And(
		jq.Match(`.status.installedComponents."%s" == false`, dashboard.LegacyComponentNameUpstream),
		jq.Match(`.status.components.dashboard.managementState == "%s"`, operatorv1.Removed),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, dashboard.ReadyConditionType, metav1.ConditionFalse),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, dashboard.ReadyConditionType, operatorv1.Removed),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, dashboard.ReadyConditionType)),
	))
}

// testEmptyManagementState tests the empty management state scenario.
func testEmptyManagementState(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	dsc := createDSCWithDashboard("")

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
	g.Expect(err).ShouldNot(HaveOccurred())

	cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       dsci,
		Conditions: conditions.NewManager(dsc, dashboard.ReadyConditionType),
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

	g.Expect(dsc).Should(WithTransform(json.Marshal, And(
		jq.Match(`.status.installedComponents."%s" == false`, dashboard.LegacyComponentNameUpstream),
		jq.Match(`.status.components.dashboard.managementState == "%s"`, operatorv1.Removed),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, dashboard.ReadyConditionType, metav1.ConditionFalse),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, dashboard.ReadyConditionType, operatorv1.Removed),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, dashboard.ReadyConditionType, common.ConditionSeverityInfo),
	)))
}

// testDashboardCRNotFound tests the Dashboard CR not found scenario.
func testDashboardCRNotFound(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	dsc := createDSCWithDashboard(operatorv1.Managed)

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
	g.Expect(err).ShouldNot(HaveOccurred())

	cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       dsci,
		Conditions: conditions.NewManager(dsc, dashboard.ReadyConditionType),
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cs).Should(Equal(metav1.ConditionFalse))
}

// testInvalidInstanceType tests the invalid instance type scenario.
func testInvalidInstanceType(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	invalidInstance := &componentApi.Dashboard{}

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
		Client:     cli,
		Instance:   invalidInstance,
		DSCI:       dsci,
		Conditions: conditions.NewManager(&dscv1.DataScienceCluster{}, dashboard.ReadyConditionType),
	})

	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("failed to convert to DataScienceCluster"))
	g.Expect(cs).Should(Equal(metav1.ConditionUnknown))
}

// testDashboardCRWithoutReadyCondition tests the Dashboard CR without Ready condition scenario.
func testDashboardCRWithoutReadyCondition(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	dsc := createDSCWithDashboard(operatorv1.Managed)
	dashboardInstance := &componentApi.Dashboard{}
	dashboardInstance.SetGroupVersionKind(gvk.Dashboard)
	dashboardInstance.SetName(componentApi.DashboardInstanceName)
	dashboardInstance.SetNamespace(testNamespace)

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dashboardInstance))
	g.Expect(err).ShouldNot(HaveOccurred())

	cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       dsci,
		Conditions: conditions.NewManager(dsc, dashboard.ReadyConditionType),
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cs).Should(Equal(metav1.ConditionFalse))
}

// testDashboardCRWithReadyConditionTrue tests Dashboard CR with Ready condition set to True.
func testDashboardCRWithReadyConditionTrue(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	dsc := createDSCWithDashboard(operatorv1.Managed)

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	// Test with ConditionTrue - function should return ConditionTrue when Ready is True
	dashboardTrue := &componentApi.Dashboard{}
	dashboardTrue.SetGroupVersionKind(gvk.Dashboard)
	dashboardTrue.SetName(componentApi.DashboardInstanceName)
	dashboardTrue.SetNamespace(testNamespace)
	dashboardTrue.Status.Conditions = []common.Condition{
		{
			Type:   status.ConditionTypeReady,
			Status: metav1.ConditionTrue,
			Reason: status.ReadyReason,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dashboardTrue))
	g.Expect(err).ShouldNot(HaveOccurred())

	cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       dsci,
		Conditions: conditions.NewManager(dsc, dashboard.ReadyConditionType),
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cs).Should(Equal(metav1.ConditionTrue))
}

// testDifferentManagementStates tests different management states.
func testDifferentManagementStates(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	managementStates := []operatorv1.ManagementState{
		operatorv1.Managed,
		operatorv1.Removed,
		operatorv1.Unmanaged,
	}

	for _, state := range managementStates {
		t.Run(string(state), func(t *testing.T) {
			dsc := createDSCWithDashboard(state)

			dsci := &dsciv1.DSCInitialization{
				Spec: dsciv1.DSCInitializationSpec{
					ApplicationsNamespace: testNamespace,
				},
			}

			cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
			g.Expect(err).ShouldNot(HaveOccurred())

			cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
				Client:     cli,
				Instance:   dsc,
				DSCI:       dsci,
				Conditions: conditions.NewManager(dsc, dashboard.ReadyConditionType),
			})

			g.Expect(err).ShouldNot(HaveOccurred())

			if state == operatorv1.Managed {
				g.Expect(cs).Should(Equal(metav1.ConditionFalse))
			} else {
				g.Expect(cs).Should(Equal(metav1.ConditionUnknown))
			}

			// Assert the expected relationship between ManagementState, Dashboard CR presence, and InstalledComponents
			assertDashboardManagedState(t, dsc, state)

			// Assert specific status fields based on management state
			switch state {
			case operatorv1.Unmanaged:
				// For Unmanaged: assert component status indicates not actively managed
				g.Expect(dsc).Should(WithTransform(json.Marshal, And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, dashboard.ReadyConditionType, metav1.ConditionFalse),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, dashboard.ReadyConditionType, operatorv1.Unmanaged),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, dashboard.ReadyConditionType, common.ConditionSeverityInfo),
				)))
			case operatorv1.Removed:
				// For Removed: assert cleanup-related status fields are set
				g.Expect(dsc).Should(WithTransform(json.Marshal, And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, dashboard.ReadyConditionType, metav1.ConditionFalse),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, dashboard.ReadyConditionType, operatorv1.Removed),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, dashboard.ReadyConditionType, common.ConditionSeverityInfo),
				)))
			}
		})
	}
}

// testNilClient tests the nil client scenario.
func testNilClient(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	dsc := createDSCWithDashboard(operatorv1.Managed)

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
		Client:     nil,
		Instance:   dsc,
		DSCI:       dsci,
		Conditions: conditions.NewManager(dsc, dashboard.ReadyConditionType),
	})

	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("client is nil"))
	g.Expect(cs).Should(Equal(metav1.ConditionUnknown))
}

// testNilInstance tests the nil instance scenario.
func testNilInstance(t *testing.T, handler *dashboard.ComponentHandler) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	cs, err := handler.UpdateDSCStatus(ctx, &types.ReconciliationRequest{
		Client:     cli,
		Instance:   nil,
		DSCI:       dsci,
		Conditions: conditions.NewManager(&dscv1.DataScienceCluster{}, dashboard.ReadyConditionType),
	})

	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("failed to convert to DataScienceCluster"))
	g.Expect(cs).Should(Equal(metav1.ConditionUnknown))
}

func createDSCWithDashboard(managementState operatorv1.ManagementState) *dscv1.DataScienceCluster {
	dsc := dscv1.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.Dashboard.ManagementState = managementState
	dsc.Status.InstalledComponents = make(map[string]bool)

	return &dsc
}

func createDashboardCR(ready bool) *componentApi.Dashboard {
	c := componentApi.Dashboard{}
	c.SetGroupVersionKind(gvk.Dashboard)
	c.SetName(componentApi.DashboardInstanceName)
	c.SetNamespace(testNamespace)

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
