package manager_test

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmanager "sigs.k8s.io/controller-runtime/pkg/manager"

	opclient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// mockManager implements manager.Manager interface for testing.
// Embeds ctrlmanager.Manager to satisfy the interface; only methods needed by tests are implemented.
//

type mockManager struct {
	ctrlmanager.Manager

	client client.Client
}

func (m *mockManager) GetClient() client.Client {
	return m.client
}

func TestNew_CreatesManagerWithWrappedClient(t *testing.T) {
	g := NewWithT(t)

	fakeClient, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	mockMgr := &mockManager{client: fakeClient}
	wrappedMgr := manager.New(mockMgr)

	g.Expect(wrappedMgr).ShouldNot(BeNil())
}

func TestGetClient_ReturnsWrappedClient(t *testing.T) {
	g := NewWithT(t)

	fakeClient, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	mockMgr := &mockManager{client: fakeClient}
	wrappedMgr := manager.New(mockMgr)

	returnedClient := wrappedMgr.GetClient()

	// It should be an *opclient.Client
	_, ok := returnedClient.(*opclient.Client)
	g.Expect(ok).Should(BeTrue(), "GetClient should return *opclient.Client")
}

func TestManager_ImplementsManagerInterface(t *testing.T) {
	// Compile-time check that Manager implements manager.Manager interface
	var _ ctrlmanager.Manager = (*manager.Manager)(nil)
}
