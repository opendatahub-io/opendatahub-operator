package registry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
)

type fakeHandler struct {
	name string
}

func (f *fakeHandler) GetName() string                                                 { return f.name }
func (f *fakeHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error { return nil }
func (f *fakeHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return nil, nil
}
func (f *fakeHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error { return nil }
func (f *fakeHandler) UpdateDSCStatus(_ context.Context, _ *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	return metav1.ConditionTrue, nil
}
func (f *fakeHandler) IsEnabled(_ *dscv2.DataScienceCluster) bool { return true }

func TestResolvedBatches_RunlevelGrouping(t *testing.T) {
	t.Parallel()

	reg := &cr.Registry{}

	reg.Add(&fakeHandler{name: "infra-a"}, cr.WithRunlevel(dag.RL(0)))
	reg.Add(&fakeHandler{name: "ai-a"}, cr.WithRunlevel(dag.RL(20)))
	reg.Add(&fakeHandler{name: "ext-a"}, cr.WithRunlevel(dag.RL(30)))
	reg.Add(&fakeHandler{name: "ai-b"}, cr.WithRunlevel(dag.RL(20)))

	batches, err := reg.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 3, "expected 3 distinct runlevel batches")

	assert.Len(t, batches[0], 1, "core-infrastructure batch")
	assert.Equal(t, "infra-a", batches[0][0].GetName())

	assert.Len(t, batches[1], 2, "core-ai batch")
	names := []string{batches[1][0].GetName(), batches[1][1].GetName()}
	assert.Contains(t, names, "ai-a")
	assert.Contains(t, names, "ai-b")

	assert.Len(t, batches[2], 1, "extensions batch")
	assert.Equal(t, "ext-a", batches[2][0].GetName())
}

func TestResolvedBatches_CrossRunlevelOrdering(t *testing.T) {
	t.Parallel()

	reg := &cr.Registry{}

	reg.Add(&fakeHandler{name: "dashboard"}, cr.WithRunlevel(dag.RL(20)))
	reg.Add(&fakeHandler{name: "trustyai"}, cr.WithRunlevel(dag.RL(30)))

	batches, err := reg.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 2)

	assert.Equal(t, "dashboard", batches[0][0].GetName())
	assert.Equal(t, "trustyai", batches[1][0].GetName())
}

func TestResolvedBatches_DisabledExcluded(t *testing.T) {
	t.Parallel()

	reg := &cr.Registry{}

	reg.Add(&fakeHandler{name: "active"}, cr.WithRunlevel(dag.RL(20)))
	reg.Add(&fakeHandler{name: "disabled"}, cr.WithRunlevel(dag.RL(20)))
	reg.Disable("disabled")

	batches, err := reg.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 1)
	require.Len(t, batches[0], 1)
	assert.Equal(t, "active", batches[0][0].GetName())
}

func TestResolvedBatches_CacheInvalidation(t *testing.T) {
	t.Parallel()

	reg := &cr.Registry{}

	reg.Add(&fakeHandler{name: "a"}, cr.WithRunlevel(dag.RL(20)))
	b1, err := reg.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, b1, 1)
	require.Len(t, b1[0], 1)

	reg.Add(&fakeHandler{name: "b"}, cr.WithRunlevel(dag.RL(30)))
	b2, err := reg.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, b2, 2, "cache should have been invalidated after Add")
}

func TestReverseBatches_Order(t *testing.T) {
	t.Parallel()

	reg := &cr.Registry{}

	reg.Add(&fakeHandler{name: "infra"}, cr.WithRunlevel(dag.RL(0)))
	reg.Add(&fakeHandler{name: "ext"}, cr.WithRunlevel(dag.RL(30)))
	reg.Add(&fakeHandler{name: "ai"}, cr.WithRunlevel(dag.RL(20)))

	batches, err := reg.ReverseBatches()
	require.NoError(t, err)
	require.Len(t, batches, 3)

	assert.Equal(t, "ext", batches[0][0].GetName(), "extensions should come first in reverse")
	assert.Equal(t, "ai", batches[1][0].GetName())
	assert.Equal(t, "infra", batches[2][0].GetName(), "infra should be last in reverse")
}

func TestForEach_DAGOrder(t *testing.T) {
	t.Parallel()

	reg := &cr.Registry{}

	reg.Add(&fakeHandler{name: "ext-a"}, cr.WithRunlevel(dag.RL(30)))
	reg.Add(&fakeHandler{name: "infra-a"}, cr.WithRunlevel(dag.RL(0)))
	reg.Add(&fakeHandler{name: "ai-a"}, cr.WithRunlevel(dag.RL(20)))

	var order []string
	err := reg.ForEach(func(ch cr.ComponentHandler) error {
		order = append(order, ch.GetName())
		return nil
	})
	require.NoError(t, err)

	require.Len(t, order, 3)
	assert.Equal(t, "infra-a", order[0])
	assert.Equal(t, "ai-a", order[1])
	assert.Equal(t, "ext-a", order[2])
}

func TestLookup_DAG(t *testing.T) {
	t.Parallel()

	reg := &cr.Registry{}

	reg.Add(&fakeHandler{name: "kserve"})

	assert.NotNil(t, reg.Lookup("kserve"))
	assert.Nil(t, reg.Lookup("nonexistent"))
}
