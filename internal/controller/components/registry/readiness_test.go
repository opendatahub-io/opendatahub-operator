package registry_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

type readinessHandler struct {
	name      string
	enabled   bool
	crObj     common.PlatformObject
	crErr     error
}

func (h *readinessHandler) GetName() string { return h.name }
func (h *readinessHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error {
	return nil
}
func (h *readinessHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return h.crObj, h.crErr
}
func (h *readinessHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error {
	return nil
}
func (h *readinessHandler) UpdateDSCStatus(_ context.Context, _ *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	return metav1.ConditionTrue, nil
}
func (h *readinessHandler) IsEnabled(_ *dscv2.DataScienceCluster) bool {
	return h.enabled
}

func newDSC() *dscv2.DataScienceCluster {
	dsc := &dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")
	dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
	return dsc
}

func TestReadinessChecker_DisabledComponent_IsReady(t *testing.T) {
	reg := &cr.Registry{}
	reg.Add(&readinessHandler{name: "disabled-comp", enabled: false})

	dsc := newDSC()
	cli, err := fakeclient.New()
	require.NoError(t, err)

	checker := cr.NewReadinessChecker(reg, cli, dsc)
	ready, err := checker.IsReady(context.Background(), "disabled-comp")

	require.NoError(t, err)
	assert.True(t, ready, "disabled component should be treated as ready")
}

func TestReadinessChecker_NilCRObject_IsReady(t *testing.T) {
	reg := &cr.Registry{}
	reg.Add(&readinessHandler{name: "nil-cr-comp", enabled: true, crObj: nil, crErr: nil})

	dsc := newDSC()
	cli, err := fakeclient.New()
	require.NoError(t, err)

	checker := cr.NewReadinessChecker(reg, cli, dsc)
	ready, err := checker.IsReady(context.Background(), "nil-cr-comp")

	require.NoError(t, err)
	assert.True(t, ready, "component with nil CR should be treated as ready")
}

func TestReadinessChecker_NewCRObjectError_TreatedAsReady(t *testing.T) {
	reg := &cr.Registry{}
	reg.Add(&readinessHandler{
		name:    "failing-comp",
		enabled: true,
		crObj:   nil,
		crErr:   fmt.Errorf("gateway domain is missing"),
	})

	dsc := newDSC()
	cli, err := fakeclient.New()
	require.NoError(t, err)

	checker := cr.NewReadinessChecker(reg, cli, dsc)
	ready, err := checker.IsReady(context.Background(), "failing-comp")

	require.NoError(t, err)
	assert.True(t, ready, "component whose CR cannot be constructed should be treated as ready for DAG gating")
}

func TestReadinessChecker_CRNotFound_NotReady(t *testing.T) {
	dashboard := &componentApi.Dashboard{}
	dashboard.SetName(componentApi.DashboardInstanceName)
	dashboard.SetGroupVersionKind(gvk.Dashboard)

	reg := &cr.Registry{}
	reg.Add(&readinessHandler{
		name:    componentApi.DashboardComponentName,
		enabled: true,
		crObj:   dashboard,
		crErr:   nil,
	})

	dsc := newDSC()
	cli, err := fakeclient.New()
	require.NoError(t, err)

	checker := cr.NewReadinessChecker(reg, cli, dsc)
	ready, err := checker.IsReady(context.Background(), componentApi.DashboardComponentName)

	require.NoError(t, err)
	assert.False(t, ready, "component whose CR does not exist on the cluster should not be ready")
}

func TestReadinessChecker_CRExistsNotReady(t *testing.T) {
	templateCR := &componentApi.Dashboard{}
	templateCR.SetName(componentApi.DashboardInstanceName)
	templateCR.SetGroupVersionKind(gvk.Dashboard)

	liveCR := &componentApi.Dashboard{}
	liveCR.SetName(componentApi.DashboardInstanceName)
	liveCR.SetGroupVersionKind(gvk.Dashboard)
	liveCR.Status.Conditions = []common.Condition{{
		Type:   status.ConditionTypeReady,
		Status: metav1.ConditionFalse,
		Reason: "NotReady",
	}}

	reg := &cr.Registry{}
	reg.Add(&readinessHandler{
		name:    componentApi.DashboardComponentName,
		enabled: true,
		crObj:   templateCR,
		crErr:   nil,
	})

	dsc := newDSC()
	cli, err := fakeclient.New(fakeclient.WithObjects(liveCR))
	require.NoError(t, err)

	checker := cr.NewReadinessChecker(reg, cli, dsc)
	ready, err := checker.IsReady(context.Background(), componentApi.DashboardComponentName)

	require.NoError(t, err)
	assert.False(t, ready, "component with Ready=False should not be ready")
}

func TestReadinessChecker_CRExistsReady(t *testing.T) {
	templateCR := &componentApi.Dashboard{}
	templateCR.SetName(componentApi.DashboardInstanceName)
	templateCR.SetGroupVersionKind(gvk.Dashboard)

	liveCR := &componentApi.Dashboard{}
	liveCR.SetName(componentApi.DashboardInstanceName)
	liveCR.SetGroupVersionKind(gvk.Dashboard)
	liveCR.Status.Conditions = []common.Condition{{
		Type:   status.ConditionTypeReady,
		Status: metav1.ConditionTrue,
		Reason: "Ready",
	}}

	reg := &cr.Registry{}
	reg.Add(&readinessHandler{
		name:    componentApi.DashboardComponentName,
		enabled: true,
		crObj:   templateCR,
		crErr:   nil,
	})

	dsc := newDSC()
	cli, err := fakeclient.New(fakeclient.WithObjects(liveCR))
	require.NoError(t, err)

	checker := cr.NewReadinessChecker(reg, cli, dsc)
	ready, err := checker.IsReady(context.Background(), componentApi.DashboardComponentName)

	require.NoError(t, err)
	assert.True(t, ready, "component with Ready=True should be ready")
}
