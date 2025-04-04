//nolint:errcheck,forcetypeassert
package mocks

import (
	"context"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
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

func (m *MockComponentHandler) UpdateDSCStatus(dsc *dscv1.DataScienceCluster, obj client.Object) error {
	return m.Called(dsc, obj).Error(0)
}

type MockController struct {
	mock.Mock
}

func (m *MockController) Owns(gvk schema.GroupVersionKind) bool {
	return m.Called(gvk).Bool(0)
}

func (m *MockController) GetClient() client.Client {
	return m.Called().Get(0).(client.Client)
}

func (m *MockController) GetDiscoveryClient() discovery.DiscoveryInterface {
	return m.Called().Get(0).(discovery.DiscoveryInterface)
}

func (m *MockController) GetDynamicClient() dynamic.Interface {
	return m.Called().Get(0).(dynamic.Interface)
}

func NewMockController(f func(m *MockController)) *MockController {
	m := new(MockController)
	f(m)

	return m
}
