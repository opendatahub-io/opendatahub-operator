package provision_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
)

func batchNames(batches [][]provision.UnifiedNode) [][]string {
	result := make([][]string, len(batches))
	for i, batch := range batches {
		names := make([]string, len(batch))
		for j, n := range batch {
			names[j] = n.GetName()
		}
		result[i] = names
	}
	return result
}

func newRegistry() *provision.UnifiedRegistry {
	return provision.NewRegistry()
}

func TestResolvedBatches_Empty(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	batches, err := r.ResolvedBatches()
	require.NoError(t, err)
	assert.Nil(t, batches)
}

func TestResolvedBatches_MixedComponentsAndModules(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("dashboard", provision.KindComponent, dag.RL(20))
	r.Add("kserve", provision.KindComponent, dag.RL(31))
	r.Add("monitoring-module", provision.KindModule, dag.RL(20))

	batches, err := r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 2)

	assert.ElementsMatch(t, []string{"dashboard", "monitoring-module"}, batchNames(batches)[0])
	assert.Equal(t, []string{"kserve"}, batchNames(batches)[1])
}

func TestResolvedBatches_CrossRunlevelOrdering(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("infra-svc", provision.KindComponent, dag.RL(0))
	r.Add("ai-module", provision.KindModule, dag.RL(20))

	batches, err := r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 2)

	assert.Equal(t, []string{"infra-svc"}, batchNames(batches)[0])
	assert.Equal(t, []string{"ai-module"}, batchNames(batches)[1])
}

func TestResolvedBatches_DisabledNodesExcluded(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("alpha", provision.KindComponent, dag.RL(20))
	r.Add("beta", provision.KindModule, dag.RL(20))
	r.Disable("beta")

	batches, err := r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.Equal(t, []string{"alpha"}, batchNames(batches)[0])
}

func TestResolvedBatches_EnableAfterDisable(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("alpha", provision.KindComponent, dag.RL(20))
	r.Disable("alpha")

	batches, err := r.ResolvedBatches()
	require.NoError(t, err)
	assert.Nil(t, batches)

	r.Enable("alpha")
	batches, err = r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.Equal(t, []string{"alpha"}, batchNames(batches)[0])
}

func TestComponentsInBatch(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("comp-a", provision.KindComponent, dag.RL(20))
	r.Add("mod-b", provision.KindModule, dag.RL(20))
	r.Add("comp-c", provision.KindComponent, dag.RL(20))

	batches, err := r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 1)

	components := provision.ComponentsInBatch(batches[0])
	assert.Len(t, components, 2)
	for _, c := range components {
		assert.Equal(t, provision.KindComponent, c.GetKind())
	}
}

func TestModulesInBatch(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("comp-a", provision.KindComponent, dag.RL(20))
	r.Add("mod-b", provision.KindModule, dag.RL(20))

	batches, err := r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 1)

	modules := provision.ModulesInBatch(batches[0])
	assert.Len(t, modules, 1)
	assert.Equal(t, "mod-b", modules[0].GetName())
	assert.Equal(t, provision.KindModule, modules[0].GetKind())
}

func TestCacheInvalidation(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("alpha", provision.KindComponent, dag.RL(20))

	b1, err := r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, b1, 1)

	r.Add("beta", provision.KindModule, dag.RL(20))

	b2, err := r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, b2, 1)
	assert.Len(t, b2[0], 2, "cache should be invalidated after Add")
}

func TestCacheInvalidation_ViaInvalidateCache(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("alpha", provision.KindComponent, dag.RL(20))

	_, err := r.ResolvedBatches()
	require.NoError(t, err)

	r.InvalidateCache()
	r.Add("beta", provision.KindComponent, dag.RL(20))

	b, err := r.ResolvedBatches()
	require.NoError(t, err)
	assert.Len(t, b[0], 2)
}

func TestReverseBatches(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("infra", provision.KindComponent, dag.RL(0))
	r.Add("core", provision.KindModule, dag.RL(20))
	r.Add("ext", provision.KindComponent, dag.RL(30))

	batches, err := r.ReverseBatches()
	require.NoError(t, err)
	require.Len(t, batches, 3)

	assert.Equal(t, []string{"ext"}, batchNames(batches)[0])
	assert.Equal(t, []string{"core"}, batchNames(batches)[1])
	assert.Equal(t, []string{"infra"}, batchNames(batches)[2])
}

func TestResolvedBatches_GranularRunlevelMixedTypes(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("comp-a", provision.KindComponent, dag.RL(20))
	r.Add("mod-b", provision.KindModule, dag.RL(25))
	r.Add("comp-c", provision.KindComponent, dag.RL(31))

	batches, err := r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 3)

	assert.Equal(t, []string{"comp-a"}, batchNames(batches)[0])
	assert.Equal(t, []string{"mod-b"}, batchNames(batches)[1])
	assert.Equal(t, []string{"comp-c"}, batchNames(batches)[2])
}

func TestResolvedBatches_SameRunlevelAlphabetical(t *testing.T) {
	t.Parallel()

	r := newRegistry()
	r.Add("zebra", provision.KindComponent, dag.RL(20))
	r.Add("alpha", provision.KindModule, dag.RL(20))

	batches, err := r.ResolvedBatches()
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.Equal(t, []string{"alpha", "zebra"}, batchNames(batches)[0])
}
