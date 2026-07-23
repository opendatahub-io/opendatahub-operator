//nolint:testpackage // Exercises package-private registry wiring directly.
package modules

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
)

type mockManager struct {
	manager.Manager

	client client.Client
	scheme *runtime.Scheme
}

func (m *mockManager) GetClient() client.Client   { return m.client }
func (m *mockManager) GetScheme() *runtime.Scheme { return m.scheme }

func TestAddDSCCompatibilityProjectorWatchesWatchesNonProjectorModules(t *testing.T) {
	withTestRegistry(t)

	DefaultRegistry().Add(provisioningModuleStub{
		moduleName: testProvisioningModuleName,
		enabled:    true,
		status:     &ModuleStatus{},
	})

	s, err := scheme.New()
	if err != nil {
		t.Fatalf("create scheme: %v", err)
	}
	cli, err := fakeclient.New(fakeclient.WithScheme(s))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}
	mgr := &mockManager{client: cli, scheme: s}

	builder := reconciler.ReconcilerFor(mgr, &dscv2.DataScienceCluster{})
	builder = AddDSCCompatibilityProjectorWatches(builder)

	watches := reflect.ValueOf(builder).Elem().FieldByName("watches")
	if got := watches.Len(); got != 1 {
		t.Fatalf("expected DSC builder to watch non-projector module CR status, got %d watches", got)
	}
}
