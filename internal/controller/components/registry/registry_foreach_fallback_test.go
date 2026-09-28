//nolint:testpackage
package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"

	. "github.com/onsi/gomega"
)

type fallbackHandler struct{ name string }

func (f *fallbackHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error {
	return nil
}
func (f *fallbackHandler) GetName() string { return f.name }
func (f *fallbackHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return nil, nil
}
func (f *fallbackHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error {
	return nil
}
func (f *fallbackHandler) UpdateDSCStatus(_ context.Context, _ *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	return metav1.ConditionTrue, nil
}
func (f *fallbackHandler) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{}
}
func (f *fallbackHandler) IsEnabled(_ *dscv2.DataScienceCluster) bool { return true }

func TestForEachFallsBackAlphabeticallyWhenDAGFails(t *testing.T) {
	g := NewWithT(t)

	orig := resolveBatches
	t.Cleanup(func() {
		resolveBatches = orig
		ctrl.SetLogger(logr.Discard())
	})
	resolveBatches = func(*Registry) ([][]HandlerEntry, error) {
		return nil, errors.New("dag failed")
	}

	var logged []string
	ctrl.SetLogger(funcr.New(func(_, args string) {
		logged = append(logged, args)
	}, funcr.Options{}))

	reg := &Registry{}
	reg.Add(&fallbackHandler{name: "zebra"}, WithRunlevel(dag.RL(0)))
	reg.Add(&fallbackHandler{name: "alpha"}, WithRunlevel(dag.RL(30)))
	reg.Add(&fallbackHandler{name: "middle"}, WithRunlevel(dag.RL(20)))
	reg.Disable("middle")

	var visited []string
	err := reg.ForEach(func(ch ComponentHandler) error {
		visited = append(visited, ch.GetName())
		return nil
	})
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visited).Should(Equal([]string{"alpha", "zebra"}))
	g.Expect(logged).To(ContainElement(And(
		ContainSubstring(`"msg"="DAG resolution failed, falling back to alphabetical order"`),
		ContainSubstring(`"controllerKind"="DataScienceCluster"`),
	)))
}
