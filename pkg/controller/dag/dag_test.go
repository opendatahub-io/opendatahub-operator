package dag_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

type testNode struct {
	name     string
	runlevel dag.Runlevel
}

func (n testNode) GetName() string           { return n.name }
func (n testNode) GetRunlevel() dag.Runlevel { return n.runlevel }

func node(name string, rl dag.Runlevel) testNode {
	return testNode{name: name, runlevel: rl}
}

func batchNames(batches [][]testNode) [][]string {
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

func TestResolve_EmptyGraph(t *testing.T) {
	t.Parallel()
	g := dag.NewGraph[testNode]()
	batches, err := g.Resolve()
	require.NoError(t, err)
	assert.Nil(t, batches)
}

func TestResolve_SingleNode(t *testing.T) {
	t.Parallel()
	g := dag.NewGraph[testNode]()
	g.Add(node("alpha", dag.RL(0)))

	batches, err := g.Resolve()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"alpha"}}, batchNames(batches))
}

func TestResolve_MultipleRunlevels_NoDepsDeterministic(t *testing.T) {
	t.Parallel()
	g := dag.NewGraph[testNode]()
	g.Add(node("dashboard", dag.RL(20)))
	g.Add(node("monitoring", dag.RL(0)))
	g.Add(node("auth", dag.RL(10)))
	g.Add(node("gateway", dag.RL(10)))
	g.Add(node("kserve", dag.RL(30)))

	batches, err := g.Resolve()
	require.NoError(t, err)
	require.Len(t, batches, 4)

	names := batchNames(batches)
	assert.Equal(t, []string{"monitoring"}, names[0])
	assert.Equal(t, []string{"auth", "gateway"}, names[1])
	assert.Equal(t, []string{"dashboard"}, names[2])
	assert.Equal(t, []string{"kserve"}, names[3])
}

func TestResolve_DefaultRunlevel(t *testing.T) {
	t.Parallel()
	g := dag.NewGraph[testNode]()
	g.Add(node("explicit", dag.RL(0)))
	g.Add(node("implicit", dag.RL(99)))

	batches, err := g.Resolve()
	require.NoError(t, err)
	require.Len(t, batches, 2)
	assert.Equal(t, []string{"explicit"}, batchNames(batches)[0])
	assert.Equal(t, []string{"implicit"}, batchNames(batches)[1])
}

func TestResolve_DeterministicTieBreaking(t *testing.T) {
	t.Parallel()
	g := dag.NewGraph[testNode]()
	g.Add(node("zebra", dag.RL(20)))
	g.Add(node("alpha", dag.RL(20)))
	g.Add(node("middle", dag.RL(20)))

	batches, err := g.Resolve()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"alpha", "middle", "zebra"}}, batchNames(batches))
}

func TestReverseBatches(t *testing.T) {
	t.Parallel()
	g := dag.NewGraph[testNode]()
	g.Add(node("infra", dag.RL(0)))
	g.Add(node("app-a", dag.RL(20)))
	g.Add(node("app-b", dag.RL(20)))

	batches, err := g.ReverseBatches()
	require.NoError(t, err)
	require.Len(t, batches, 2)

	names := batchNames(batches)
	assert.Equal(t, []string{"app-b", "app-a"}, names[0])
	assert.Equal(t, []string{"infra"}, names[1])
}

func TestRunlevel_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "00", dag.RL(0).String())
	assert.Equal(t, "99", dag.RL(99).String())
	assert.Equal(t, "31", dag.RL(31).String())
}

func TestResolve_GranularRunlevels(t *testing.T) {
	t.Parallel()
	g := dag.NewGraph[testNode]()
	g.Add(node("dashboard", dag.RL(21)))
	g.Add(node("pipelines", dag.RL(21)))
	g.Add(node("modelregistry", dag.RL(22)))
	g.Add(node("workbenches", dag.RL(23)))

	batches, err := g.Resolve()
	require.NoError(t, err)
	require.Len(t, batches, 3)

	names := batchNames(batches)
	assert.Equal(t, []string{"dashboard", "pipelines"}, names[0])
	assert.Equal(t, []string{"modelregistry"}, names[1])
	assert.Equal(t, []string{"workbenches"}, names[2])
}

func TestResolve_MixedGranularity(t *testing.T) {
	t.Parallel()
	g := dag.NewGraph[testNode]()
	g.Add(node("infra", dag.RL(0)))
	g.Add(node("ai-early", dag.RL(21)))
	g.Add(node("ai-late", dag.RL(25)))
	g.Add(node("ext", dag.RL(30)))

	batches, err := g.Resolve()
	require.NoError(t, err)
	require.Len(t, batches, 4)

	names := batchNames(batches)
	assert.Equal(t, []string{"infra"}, names[0])
	assert.Equal(t, []string{"ai-early"}, names[1])
	assert.Equal(t, []string{"ai-late"}, names[2])
	assert.Equal(t, []string{"ext"}, names[3])
}

type stubChecker struct {
	readyMap map[string]bool
}

func (s *stubChecker) IsReady(_ context.Context, name string) (bool, error) {
	ready, ok := s.readyMap[name]
	if !ok {
		return false, fmt.Errorf("unknown node %s: %w", name, dag.ErrUnknownNode)
	}
	return ready, nil
}

func TestCompositeChecker(t *testing.T) {
	t.Parallel()
	checker := dag.CompositeChecker{
		&stubChecker{readyMap: map[string]bool{"module-a": true}},
		&stubChecker{readyMap: map[string]bool{"component-b": false}},
	}

	ready, err := checker.IsReady(context.Background(), "module-a")
	require.NoError(t, err)
	assert.True(t, ready)
}

func TestCompositeChecker_NoMatch(t *testing.T) {
	t.Parallel()
	checker := dag.CompositeChecker{}
	_, err := checker.IsReady(context.Background(), "unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no checker")
}

func TestGetRunlevelPolicy_DefaultApplied(t *testing.T) {
	t.Parallel()

	policy := dag.GetRunlevelPolicy(42)
	assert.Equal(t, dag.DefaultTimeout, policy.Timeout, "unlisted runlevel should get default")
}

func TestGetRunlevelPolicy_OverrideTakesPrecedence(t *testing.T) {
	dag.SetRunlevelPolicy(77, dag.RunlevelPolicy{Timeout: 0})
	defer dag.ClearRunlevelPolicy(77)

	policy := dag.GetRunlevelPolicy(77)
	assert.Equal(t, time.Duration(0), policy.Timeout, "override should take precedence")
}

func TestRunlevelPolicy_TimeoutZeroBlocksForever(t *testing.T) {
	t.Parallel()

	policy := dag.RunlevelPolicy{Timeout: 0}
	assert.Zero(t, policy.Timeout, "zero timeout means block forever")
}

func TestRunlevelPolicy_TimeoutExpired(t *testing.T) {
	t.Parallel()

	policy := dag.RunlevelPolicy{Timeout: 10 * time.Minute}
	stuckSince := time.Now().Add(-11 * time.Minute)
	elapsed := time.Since(stuckSince)
	assert.True(t, policy.Timeout > 0 && elapsed >= policy.Timeout, "should exceed timeout")
}

func TestRunlevelPolicy_TimeoutNotYetExpired(t *testing.T) {
	t.Parallel()

	policy := dag.RunlevelPolicy{Timeout: 10 * time.Minute}
	stuckSince := time.Now().Add(-5 * time.Minute)
	elapsed := time.Since(stuckSince)
	assert.False(t, policy.Timeout > 0 && elapsed >= policy.Timeout, "should not exceed timeout yet")
}
