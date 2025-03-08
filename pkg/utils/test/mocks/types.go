//nolint:errcheck,forcetypeassert
package mocks

import (
	"context"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type MockComponentHandler struct {
	mock.Mock
}

func (m *MockComponentHandler) Init(platform common.Platform) error {
	return m.Called(platform).Error(0)
}

func (m *MockComponentHandler) GetName() string {
	return m.Called().String(0)
}

func (m *MockComponentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	return m.Called(dsc).Get(0).(operatorv1.ManagementState)
}

func (m *MockComponentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	return m.Called(dsc).Get(0).(common.PlatformObject)
}

func (m *MockComponentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	return m.Called(ctx, mgr).Error(0)
}

func (m *MockComponentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	c := m.Called(ctx, rr)
	return c.Get(0).(metav1.ConditionStatus), c.Error(1)
}
