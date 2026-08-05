package registry_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"

	. "github.com/onsi/gomega"
)

var testGVK = schema.GroupVersionKind{
	Group:   "test.example.com",
	Version: "v1",
	Kind:    "TestComponent",
}

type readinessHandler struct {
	name    string
	enabled bool
}

func (f *readinessHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error {
	return nil
}
func (f *readinessHandler) GetName() string { return f.name }
func (f *readinessHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return nil, nil
}
func (f *readinessHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error {
	return nil
}
func (f *readinessHandler) UpdateDSCStatus(_ context.Context, _ *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	return metav1.ConditionTrue, nil
}
func (f *readinessHandler) IsEnabled(_ *dscv2.DataScienceCluster) bool { return f.enabled }

type readinessHandlerWithNamer struct {
	readinessHandler

	instanceName string
	instanceGVK  schema.GroupVersionKind
}

func (f *readinessHandlerWithNamer) GetInstanceName() string                 { return f.instanceName }
func (f *readinessHandlerWithNamer) GetInstanceGVK() schema.GroupVersionKind { return f.instanceGVK }

var _ registry.InstanceNamer = (*readinessHandlerWithNamer)(nil)

func newFakeClient(scheme *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func readinessTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = dscv2.AddToScheme(s)
	return s
}

func TestReadinessChecker_NilDSC_SuppressedComponent_IsReady(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&readinessHandler{name: "suppressed", enabled: true})
	reg.Disable("suppressed")

	checker := registry.NewReadinessChecker(reg, newFakeClient(readinessTestScheme()), nil)
	ready, err := checker.IsReady(context.Background(), "suppressed")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeTrue(), "suppressed component should be treated as ready")
}

func TestReadinessChecker_NilDSC_EnabledNoInstanceNamer_IsReady(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&readinessHandler{name: "no-namer", enabled: true})

	checker := registry.NewReadinessChecker(reg, newFakeClient(readinessTestScheme()), nil)
	ready, err := checker.IsReady(context.Background(), "no-namer")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeTrue(), "handler without InstanceNamer should be treated as ready")
}

func TestReadinessChecker_NilDSC_CRNotFound_NotReady(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&readinessHandlerWithNamer{
		readinessHandler: readinessHandler{name: "comp-a", enabled: true},
		instanceName:     "default-comp-a",
		instanceGVK:      testGVK,
	})

	cli := newFakeClient(readinessTestScheme())
	checker := registry.NewReadinessChecker(reg, cli, nil)
	ready, err := checker.IsReady(context.Background(), "comp-a")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeFalse(), "CR not found should mean not ready")
}

func TestReadinessChecker_NilDSC_CRExistsReadinessStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		condStatus  metav1.ConditionStatus
		expectReady bool
	}{
		{"Ready=True means ready", metav1.ConditionTrue, true},
		{"Ready=False means not ready", metav1.ConditionFalse, false},
		{"Ready=Unknown means not ready", metav1.ConditionUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			cr := &unstructured.Unstructured{}
			cr.SetGroupVersionKind(testGVK)
			cr.SetName("default-comp")
			_ = unstructured.SetNestedSlice(cr.Object, []any{
				map[string]any{
					"type":   status.ConditionTypeReady,
					"status": string(tt.condStatus),
				},
			}, "status", "conditions")

			reg := &registry.Registry{}
			reg.Add(&readinessHandlerWithNamer{
				readinessHandler: readinessHandler{name: "comp", enabled: true},
				instanceName:     "default-comp",
				instanceGVK:      testGVK,
			})

			cli := newFakeClient(readinessTestScheme(), cr)
			checker := registry.NewReadinessChecker(reg, cli, nil)
			ready, err := checker.IsReady(context.Background(), "comp")
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(ready).Should(Equal(tt.expectReady))
		})
	}
}

func TestReadinessChecker_NilDSC_UnknownNode(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &registry.Registry{}

	checker := registry.NewReadinessChecker(reg, newFakeClient(readinessTestScheme()), nil)
	_, err := checker.IsReady(context.Background(), "nonexistent")
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err).Should(MatchError(ContainSubstring(dag.ErrUnknownNode.Error())))
}
